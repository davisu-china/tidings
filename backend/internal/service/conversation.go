package service

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/jwtutil"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
	"github.com/davisu-china/tidings/backend/internal/repo"
	"github.com/davisu-china/tidings/backend/internal/ws"
)

// 会话（§4.7）。匹配成立之后两个人之间的唯一通道。
//
// 这里没有「建会话」这个方法：matches 那一行就是会话，它由引荐的
// 结算创建（见 respond.go）。所以这一层全是「已经有一条会话了，
// 拿它做什么」。

const (
	// msgMaxRunes 是一条消息的长度上限，按字符数不是字节数。
	// 表上的 CHECK 用的也是 char_length，两边同一把尺子 ——
	// 中文一个字三字节，按字节算的话 334 个字就被拒了。
	msgMaxRunes = 1000

	// clientMsgIDMax 是幂等键的长度上限。uuid 是 36，
	// 留出余量给别的客户端实现。
	clientMsgIDMax = 64

	// 一页拉多少条消息，以及客户端最多能要多少。
	msgPageDefault = 30
	msgPageMax     = 100

	// wsTicketTTL 是实时通道入场券的有效期（§18.3：60 秒）。
	//
	// 这么短是因为它要出现在 URL 里：URL 会进代理日志、会进浏览器
	// 历史，而这张票能直接换到一条读得到全部会话的通道。
	// 有效期够走完「拿票 → 连上」这一趟就够。
	wsTicketTTL = 60 * time.Second
)

// ---------------------------------------------------------------- 视图

// MatchView 是会话列表里的一条。
type MatchView struct {
	MatchID int64  `json:"match_id"`
	OtherID int64  `json:"other_id"`
	Status  string `json:"status"`

	Nickname *string `json:"nickname"`
	CoverURL string  `json:"cover_url"`

	CreatedAt time.Time `json:"created_at"`
	// LastMsgAt 为 nil 表示还没人说过话。列表排序按它，
	// 客户端显示时间也按它 —— 没有消息时显示匹配成立的时间。
	LastMsgAt *time.Time `json:"last_msg_at"`

	// 最后一条消息的摘要。三个字段一起为空 = 还没有消息。
	LastContent *string `json:"last_content"`
	LastMine    *bool   `json:"last_mine"`

	Unread int `json:"unread"`
}

// MatchListView 是 GET /matches 的响应。
//
// Unread 是给底部导航的角标用的合计。不要求客户端把列表拉全再自己加
// —— 会话列表将来可能分页，那时候前端自己数就会数错。
type MatchListView struct {
	Matches []MatchView `json:"matches"`
	Unread  int         `json:"unread"`
}

// MessageView 是一条消息。Mine 是「以调用者为视角」的方向，
// 由 service 填 —— repo 不知道谁在问。
type MessageView struct {
	ID          int64     `json:"id"`
	MatchID     int64     `json:"match_id"`
	SenderID    int64     `json:"sender_id"`
	Mine        bool      `json:"mine"`
	Content     string    `json:"content"`
	ClientMsgID string    `json:"client_msg_id"`
	CreatedAt   time.Time `json:"created_at"`
}

// MessageListView 是 GET /matches/:id/messages 的响应。
//
// 带上 Other：聊天页顶部要显示对方是谁，而它只会为这一条会话
// 拉一次消息列表，不该再多打一个接口去问「这个 match_id 是谁」。
type MessageListView struct {
	Messages []MessageView `json:"messages"`
	// HasMore 表示这一页之前还有更早的消息，客户端据此决定
	// 要不要显示「加载更早」。游标分页没法给总页数（§18.3）。
	HasMore bool      `json:"has_more"`
	Other   MatchView `json:"other"`
}

// WSTicketView 是 GET /auth/ws-ticket 的响应。
type WSTicketView struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ---------------------------------------------------------------- 列表

// ListMatches 列出本人的会话。
func (s *Service) ListMatches(ctx context.Context, user *model.User) (*MatchListView, error) {
	rows, err := s.Repo.ListMatches(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	out := make([]MatchView, 0, len(rows))
	total := 0
	for _, r := range rows {
		v := s.toMatchView(r, user.ID)
		total += v.Unread
		out = append(out, v)
	}
	return &MatchListView{Matches: out, Unread: total}, nil
}

func (s *Service) toMatchView(r repo.MatchRow, uid int64) MatchView {
	cover := ""
	if r.CoverKey != nil {
		cover = s.ImgURL(*r.CoverKey, storage.VariantThumb)
	}
	v := MatchView{
		MatchID:     r.ID,
		OtherID:     r.OtherID,
		Status:      r.Status,
		Nickname:    r.Nickname,
		CoverURL:    cover,
		CreatedAt:   r.CreatedAt,
		LastMsgAt:   r.LastMsgAt,
		LastContent: r.LastContent,
		Unread:      r.Unread,
	}
	if r.LastSenderID != nil {
		mine := *r.LastSenderID == uid
		v.LastMine = &mine
	}
	return v
}

// ---------------------------------------------------------------- 消息

// ListMessages 按游标往前翻一页消息。
//
// beforeID 为 0 表示「最新的一页」。返回的 messages 按时间正序
// （最早的在前），因为前端是往下追加渲染的 —— 查询为了用游标
// 必然是倒序取，这里翻一次，让调用方拿到能直接渲染的顺序。
func (s *Service) ListMessages(
	ctx context.Context, user *model.User, matchID, beforeID int64, limit int,
) (*MessageListView, error) {
	m, err := s.Repo.GetMatch(ctx, user.ID, matchID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		// 不是当事人就是不存在，同一个答案（与引荐的反枚举规则一致）
		return nil, apierr.ErrMatchNotFound
	}

	if limit <= 0 {
		limit = msgPageDefault
	}
	if limit > msgPageMax {
		limit = msgPageMax
	}

	rows, err := s.Repo.ListMessages(ctx, matchID, beforeID, limit)
	if err != nil {
		return nil, err
	}

	// ListMessages 多取了一条用来判断还有没有更早的
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	out := make([]MessageView, 0, len(rows))
	// 倒着遍历，让结果正序
	for i := len(rows) - 1; i >= 0; i-- {
		out = append(out, toMessageView(rows[i], user.ID))
	}

	return &MessageListView{
		Messages: out,
		HasMore:  hasMore,
		Other:    s.toMatchView(*m, user.ID),
	}, nil
}

func toMessageView(r repo.MessageRow, uid int64) MessageView {
	return MessageView{
		ID:          r.ID,
		MatchID:     r.MatchID,
		SenderID:    r.SenderID,
		Mine:        r.SenderID == uid,
		Content:     r.Content,
		ClientMsgID: r.ClientMsgID,
		CreatedAt:   r.CreatedAt,
	}
}

// SendMessage 发一条消息。
//
// clientMsgID 由客户端生成，是幂等的全部实现：同一个 id 重发不会
// 产生第二条消息，只会把原来那条原样返回。前端的重试、用户连点
// 发送、网络抖动重发，都靠它收敛到一条。
func (s *Service) SendMessage(
	ctx context.Context, user *model.User, matchID int64, clientMsgID, content string,
) (*MessageView, error) {
	clientMsgID = strings.TrimSpace(clientMsgID)
	if clientMsgID == "" || len(clientMsgID) > clientMsgIDMax {
		return nil, apierr.ErrBadRequest.WithMessage("缺少有效的 client_msg_id")
	}

	// 两端空白去掉再判：全是空格的消息在界面上是一条空气泡，
	// 存进去之后谁也说不清它是什么。去掉空白也让「  你好  」和
	// 「你好」是同一个长度。
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, apierr.ErrMessageEmpty
	}
	if utf8.RuneCountInString(content) > msgMaxRunes {
		return nil, apierr.ErrMessageTooLong
	}

	m, err := s.Repo.GetMatch(ctx, user.ID, matchID)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, apierr.ErrMatchNotFound
	}
	if m.Status != model.MatchActive {
		return nil, apierr.ErrBlocked
	}

	var msg repo.MessageRow
	err = s.Repo.Tx(func(tx *gorm.DB) error {
		id, created, err := s.Repo.InsertMessage(tx, repo.MessageRow{
			MatchID:     matchID,
			SenderID:    user.ID,
			Content:     content,
			ClientMsgID: clientMsgID,
		})
		if err != nil {
			return err
		}
		if !created {
			// 重发。把原来那条查回来原样返回，不报错 ——
			// 用户点了发送、网络抖了一下、前端重试，他期待的结果
			// 是「消息在」，不是一句「你已经发过了」。
			old, err := s.Repo.GetMessageByClientID(ctx, matchID, clientMsgID)
			if err != nil {
				return err
			}
			if old == nil {
				// 唯一冲突了却查不到：冲突来自另一条 INSERT 且它
				// 还没提交，或者刚好被删了。都极罕见，但把 nil
				// 当成一条消息用会直接 panic。
				return apierr.ErrInternal
			}
			msg = *old
			return nil
		}

		// 推进会话排序用的时间戳，与插入同一个事务。
		// 分开做的话，中间挂掉会让这条会话沉到列表下面，
		// 而消息本身是好的 —— 这种不一致没有任何报错。
		if err := s.Repo.TouchLastMsg(tx, matchID, time.Now()); err != nil {
			return err
		}
		msg = repo.MessageRow{
			ID:          id,
			MatchID:     matchID,
			SenderID:    user.ID,
			Content:     content,
			ClientMsgID: clientMsgID,
			CreatedAt:   time.Now(),
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	view := toMessageView(msg, user.ID)

	// 只在真的插进去时推。重发不推：对方已经收到过一次了。
	if s.Events != nil {
		// 两边都推，包括发送方自己。发送方在别的标签页、别的设备上
		// 也开着这条会话，而那一边不知道刚才发生了什么。
		// 发起的那一个标签页会拿到自己刚发的那条 —— 前端按
		// client_msg_id 去重，多收一次是幂等的。
		for _, uid := range []int64{user.ID, m.OtherID} {
			// 每个人按自己的视角各造一份。mine 是「这条是不是我发的」，
			// 只有相对某个收件人才有意义 —— 共用一份的话，收件人拿到
			// 的是发送方的视角，别人发来的消息会带着 mine: true 过来。
			payload := map[string]any{
				"match_id": matchID,
				"message":  toMessageView(msg, uid),
			}
			if err := s.Events.Publish(ctx, uid, ws.Event{Type: ws.EventMessage, Data: payload}); err != nil {
				s.Log.WarnContext(ctx, "推送消息事件失败",
					slog.Int64("user_id", uid), slog.Any("err", err))
			}
		}
	}
	return &view, nil
}

// ---------------------------------------------------------------- 已读

// MarkRead 把一个人的已读水位推到最新（或客户端指定的那一条）。
//
// 已读只存水位不存明细（§10.6 的 match_reads）：要的是「有没有
// 没读的」和角标上的数字，而明细会随消息量线性增长，且随时可以
// 由水位算出来。
func (s *Service) MarkRead(ctx context.Context, user *model.User, matchID, lastMsgID int64) (int64, error) {
	m, err := s.Repo.GetMatch(ctx, user.ID, matchID)
	if err != nil {
		return 0, err
	}
	if m == nil {
		return 0, apierr.ErrMatchNotFound
	}
	if m.Status != model.MatchActive {
		return 0, apierr.ErrBlocked
	}

	max, err := s.Repo.LastMessageID(ctx, matchID)
	if err != nil {
		return 0, err
	}
	// 客户端说「我读到最新了」时给 0，由服务端填上。
	if lastMsgID <= 0 || lastMsgID > max {
		// 上界要卡住：水位是 GREATEST 上去的，客户端报一个很大的
		// id 就能把之后所有消息都标成已读，而它一条都没看过。
		lastMsgID = max
	}

	if err := s.Repo.MarkRead(ctx, matchID, user.ID, lastMsgID); err != nil {
		return 0, err
	}
	return lastMsgID, nil
}

// ---------------------------------------------------------------- 实时凭据

// 实时通道入场券在 Redis 里的键。票据本身已经签过名，这一笔的
// 作用是「用过就作废」：票据在 URL 里，会进代理日志和浏览器历史，
// 交出去之后不该还能再连一次。
func wsTicketKey(jti string) string { return "ws:ticket:" + jti }

// CreateWSTicket 签发一张实时通道的入场券。
//
// 不给长期 token 是因为 WebSocket 没法自定义请求头（浏览器的
// WebSocket API 只让带 URL 和子协议），凭据只能出现在 query 里。
// 把 access token 放进 query 等于每一条代理日志里都有一份可用的
// 长期凭据。
func (s *Service) CreateWSTicket(ctx context.Context, user *model.User) (*WSTicketView, error) {
	ticket, jti, expiresAt, err := s.JWT.Issue(user.ID, user.TokenVersion, jwtutil.TypeWSTicket)
	if err != nil {
		return nil, err
	}
	// NX：同一张票只登记一次。jti 是 uuid，撞上的概率可以忽略，
	// 真撞上时宁可让这一次签发失败，也不能覆盖掉别人那张还没用的票。
	ok, err := s.Repo.Redis.SetNX(ctx, wsTicketKey(jti), 1, wsTicketTTL).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apierr.ErrWSTicketInvalid
	}
	return &WSTicketView{Ticket: ticket, ExpiresAt: expiresAt}, nil
}

// ConsumeWSTicket 核销一张入场券，返回它属于谁。
//
// GETDEL 而不是先 GET 再 DEL：两个并发的握手会同时读到「在」，
// 一次性就破了。原子地取走才是「只能用一次」。
func (s *Service) ConsumeWSTicket(ctx context.Context, ticket string) (*model.User, error) {
	claims, err := s.JWT.Parse(ticket, jwtutil.TypeWSTicket)
	if err != nil {
		return nil, apierr.ErrWSTicketInvalid
	}
	n, err := s.Repo.Redis.GetDel(ctx, wsTicketKey(claims.ID)).Result()
	if err != nil || n == "" {
		return nil, apierr.ErrWSTicketInvalid
	}

	// 回查一次用户：60 秒里他可能被封了、被重置了密码。
	// 与 RequireAuth 同一套判断，只是错误码统一成「凭证无效」——
	// 握手阶段没有地方给用户看具体原因。
	user, err := s.LoadUser(ctx, claims.UserID)
	if err != nil {
		return nil, apierr.ErrWSTicketInvalid
	}
	if user.TokenVersion != claims.TokenVersion || user.Status == model.StatusBanned {
		return nil, apierr.ErrWSTicketInvalid
	}
	return user, nil
}
