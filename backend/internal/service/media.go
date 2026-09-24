package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // 注册 JPEG 解码器
	_ "image/png"  // 注册 PNG 解码器
	"io"
	"path"
	"strings"

	"github.com/disintegration/imaging"
	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/facedetect"
	"github.com/davisu-china/tidings/backend/internal/pkg/storage"
)

const (
	maxUploadBytes = 8 << 20 // 8MB，前端会先压到 2MB 以内
	maxPhotos      = 9
	jpegQuality    = 82
)

// 接受的输入格式。输出永远是 JPEG 三档（原因见 storage 包的注释）。
//
// 收 PNG 是因为它在输入侧零成本：image/png 是标准库，不引入 cgo，
// 而用户从截图、微信另存出来的图有相当一部分就是 PNG。
// 因为一个可以顺手转掉的格式把用户挡在门外没有道理。
var allowedExt = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
}

// UploadTicket 是直传凭据。客户端拿到后自己 PUT 到 UploadURL，
// 图片字节不经过 API 服务。
type UploadTicket struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
	ExpiresIn int    `json:"expires_in"`
	MaxBytes  int64  `json:"max_bytes"`
}

// PhotoView 是照片的对外表示。
//
// URL 由服务端拼（ImgURL），不落库：换 CDN 域名时不该要跑一次数据迁移。
type PhotoView struct {
	ID        int64  `json:"id"`
	Position  int16  `json:"position"`
	ObjectKey string `json:"object_key"`
	ThumbURL  string `json:"thumb_url"`
	CardURL   string `json:"card_url"`
	FullURL   string `json:"full_url"`
}

// CreateUploadTicket 生成预签名直传 URL。
//
// 只发 URL，不在此时写库 —— 用户可能拿了 URL 却不上传。
// 真正的登记发生在 SetAvatar / AddPhoto，那时对象已经存在。
func (s *Service) CreateUploadTicket(ctx context.Context, user *model.User, ext string) (*UploadTicket, error) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if !allowedExt[ext] {
		return nil, apierr.ErrUploadBadType
	}

	key := storage.NewObjectKey(user.ID, ext)
	u, err := s.Storage.PresignPut(ctx, key)
	if err != nil {
		s.Log.Error("生成预签名 URL 失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	return &UploadTicket{
		UploadURL: u.String(),
		ObjectKey: key,
		ExpiresIn: int(s.Cfg.MinIO.PresignExpiry.Seconds()),
		MaxBytes:  maxUploadBytes,
	}, nil
}

// SetAvatar 把已上传的对象登记为头像，并生成三档派生图。
//
// 头像必须有人脸：它是引荐卡上唯一的脸，也是「账号背后是个真人」
// 唯一能在 M1 自动执行的检查（见 §4.1）。
func (s *Service) SetAvatar(ctx context.Context, user *model.User, objectKey string) (string, error) {
	if err := s.processUpload(ctx, user, objectKey, true); err != nil {
		return "", err
	}

	var prev string
	err := s.Repo.Tx(func(tx *gorm.DB) error {
		var p model.Profile
		if err := tx.First(&p, "user_id = ?", user.ID).Error; err != nil {
			return err
		}
		prev = p.AvatarKey
		// 旧头像留着（AvatarPrevKey），审核拒绝时能回退。
		// 新头像在人工过目之前标记 pending —— 但 M1 里它不挡任何东西，
		// 人工队列要到 M5 才有。
		return tx.Model(&model.Profile{}).Where("user_id = ?", user.ID).Updates(map[string]any{
			"avatar_key":          objectKey,
			"avatar_prev_key":     prev,
			"avatar_review_state": model.ReviewPending,
		}).Error
	})
	if err != nil {
		s.Log.Error("登记头像失败", "err", err, "uid", user.ID)
		return "", apierr.ErrInternal
	}

	// 换了头像可能刚好凑齐入池条件（头像本身就是必填 9 项之一）
	s.syncAfterMedia(ctx, user)

	if prev != "" && prev != objectKey {
		// 删失败只是留下一个孤儿对象，不影响用户，记日志即可
		if err := s.removeObjects(ctx, prev); err != nil {
			s.Log.Warn("清理旧头像失败", "err", err, "key", prev)
		}
	}
	return objectKey, nil
}

// AddPhoto 登记一张照片，排在当前最后一个位置之后。
//
// photos 上有 UNIQUE (user_id, position) DEFERRABLE INITIALLY DEFERRED ——
// 拖拽排序靠它才能一次提交整个新顺序。代价是 ON CONFLICT 用不了
// （延迟约束在语句结束时还没生效），所以这里必须「先查再改」，
// 而且要和插入放在同一个事务里，否则并发下两个请求会抢同一个 position。
func (s *Service) AddPhoto(ctx context.Context, user *model.User, objectKey string) (*PhotoView, error) {
	if err := s.processUpload(ctx, user, objectKey, false); err != nil {
		return nil, err
	}

	var photo model.Photo
	err := s.Repo.Tx(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&model.Photo{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count >= maxPhotos {
			return apierr.ErrPhotoLimit
		}

		// 取当前最小的空位，而不是用 count：删过中间的照片之后，
		// count 会和实际占用的位置号错开，插进去就是重复 position。
		var used []int16
		if err := tx.Model(&model.Photo{}).Where("user_id = ?", user.ID).
			Pluck("position", &used).Error; err != nil {
			return err
		}
		taken := make(map[int16]bool, len(used))
		for _, p := range used {
			taken[p] = true
		}
		var pos int16
		for i := int16(0); i < maxPhotos; i++ {
			if !taken[i] {
				pos = i
				break
			}
		}

		photo = model.Photo{
			UserID:      user.ID,
			ObjectKey:   objectKey,
			Position:    pos,
			ReviewState: model.PhotoUnreviewed,
		}
		return tx.Create(&photo).Error
	})
	if err != nil {
		var apiErr *apierr.Error
		if errors.As(err, &apiErr) {
			return nil, apiErr
		}
		s.Log.Error("登记照片失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	// 第 1 张落库就可能凑齐入池条件（门槛是 1 张）
	s.syncAfterMedia(ctx, user)
	return s.toPhotoView(&photo), nil
}

// ListPhotos 按顺序返回全部照片。
func (s *Service) ListPhotos(ctx context.Context, userID int64) ([]PhotoView, error) {
	var photos []model.Photo
	if err := s.Repo.DB.WithContext(ctx).
		Where("user_id = ?", userID).Order("position ASC").Find(&photos).Error; err != nil {
		s.Log.Error("读取照片列表失败", "err", err, "uid", userID)
		return nil, apierr.ErrInternal
	}
	out := make([]PhotoView, 0, len(photos))
	for i := range photos {
		out = append(out, *s.toPhotoView(&photos[i]))
	}
	return out, nil
}

// ReorderPhotos 按传入的 id 顺序重排，position = 下标。
//
// 一次提交整个新顺序，不做增量移动：拖拽的中间态没有意义，
// 而完整顺序是唯一不会产生重复 position 的输入。
func (s *Service) ReorderPhotos(ctx context.Context, user *model.User, ids []int64) ([]PhotoView, error) {
	var photos []model.Photo
	if err := s.Repo.DB.WithContext(ctx).
		Where("user_id = ?", user.ID).Find(&photos).Error; err != nil {
		s.Log.Error("读取照片失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	// 必须提交完整且不重复的集合：只发一部分的话，没提到的照片
	// position 不变，可能和新的下标撞车。
	if len(ids) != len(photos) {
		return nil, apierr.ErrBadRequest.WithMessage("请提交完整的照片顺序")
	}
	owned := make(map[int64]bool, len(photos))
	for _, p := range photos {
		owned[p.ID] = true
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if !owned[id] || seen[id] {
			return nil, apierr.ErrNotFound
		}
		seen[id] = true
	}

	err := s.Repo.Tx(func(tx *gorm.DB) error {
		// 逐行写成目标位置即可 —— 中间态一定会有重复 position
		// （把第 0 张挪到第 2 位，途中它和第 2 张同号），
		// 之所以不报错，是因为 photos 上那条唯一约束声明成了
		// DEFERRABLE INITIALLY DEFERRED：检查推迟到 COMMIT，
		// 只校验最终状态。这条约束一旦被改成 IMMEDIATE，这里立刻会炸。
		for i, id := range ids {
			if err := tx.Model(&model.Photo{}).
				Where("id = ? AND user_id = ?", id, user.ID).
				Update("position", int16(i)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.Log.Error("重排照片失败", "err", err, "uid", user.ID)
		return nil, apierr.ErrInternal
	}

	return s.ListPhotos(ctx, user.ID)
}

// DeletePhoto 删除一张照片并把它后面的位置前移，保持 0..n-1 连续。
//
// 不存在与不属于你都返回 404：返回 403 等于告诉对方「这个 id 存在」。
func (s *Service) DeletePhoto(ctx context.Context, user *model.User, photoID int64) error {
	var photo model.Photo
	if err := s.Repo.DB.WithContext(ctx).
		Where("id = ? AND user_id = ?", photoID, user.ID).First(&photo).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apierr.ErrNotFound
		}
		s.Log.Error("读取照片失败", "err", err, "uid", user.ID)
		return apierr.ErrInternal
	}

	err := s.Repo.Tx(func(tx *gorm.DB) error {
		// 最后一张不给删。
		//
		// 入池门槛是 1 张，所以「只有 1 张」是这个产品的典型形态，不是边角。
		// 而删照片**不会**把 status 退回 onboarding（见下面 syncAfterMedia 的
		// 说明，那是有意的），于是一个手滑就能造出「在池子里、但没有封面」的
		// 用户 —— 别人收件箱里那张卡会退化成没有照片的信。
		//
		// 事前拦住比事后退让便宜得多：换照片的路径是「先传新的，再删旧的」，
		// 新照片会自然顶上封面（AddPhoto 取最小空位，删除又把 position 前移）。
		// 从 2 张减到 1 张是合法的，所以判的是 <= 1 而不是 < minPhotosForActive。
		var count int64
		if err := tx.Model(&model.Photo{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil {
			return err
		}
		if count <= 1 {
			return apierr.ErrLastPhoto
		}

		if err := tx.Delete(&model.Photo{}, photo.ID).Error; err != nil {
			return err
		}
		// 把空隙填上，保持 position 是 0..n-1。整条 UPDATE 一步到位，
		// 靠的同样是那条 DEFERRABLE 的唯一约束（见 ReorderPhotos）。
		return tx.Model(&model.Photo{}).
			Where("user_id = ? AND position > ?", user.ID, photo.Position).
			Update("position", gorm.Expr("position - 1")).Error
	})
	if err != nil {
		var apiErr *apierr.Error
		if errors.As(err, &apiErr) {
			return apiErr
		}
		s.Log.Error("删除照片失败", "err", err, "uid", user.ID)
		return apierr.ErrInternal
	}

	if err := s.removeObjects(ctx, photo.ObjectKey); err != nil {
		// 对象没删掉只是留了垃圾，照片记录已经没了，用户看到的是成功
		s.Log.Warn("清理照片对象失败", "err", err, "key", photo.ObjectKey)
	}

	// 这里不会把 status 改回 onboarding —— 用户已经入池过，已经建立的引荐
	// 不该因为他删了张照片就失效（applyProfileState 对非 onboarding 直接
	// return）。所以「掉到 0 张」这件事必须由上面那道 count <= 1 拦住，
	// 而不是指望这里退回建档流程。
	s.syncAfterMedia(ctx, user)
	return nil
}

// processUpload 在登记之前校验对象确实存在、确实是图片、够不够格。
//
// 关键点：预签名 URL 允许客户端往这个 key 写任何东西，
// 所以「上传过了」完全不能当作「内容是图片」的证据。必须拉回来解一遍。
func (s *Service) processUpload(ctx context.Context, user *model.User, objectKey string, requireFace bool) error {
	// 前端拿到预签名 URL 之后可以提交任意 key，
	// 不校验的话就能把别人的照片登记成自己的
	if !storage.OwnsKey(user.ID, objectKey) {
		return apierr.ErrForbidden
	}
	if !allowedExt[strings.ToLower(path.Ext(objectKey))] {
		return apierr.ErrUploadBadType
	}

	obj, err := s.Storage.Get(ctx, objectKey)
	if err != nil {
		s.Log.Error("读取上传对象失败", "err", err, "key", objectKey)
		return apierr.ErrInternal
	}
	defer obj.Close()

	info, err := obj.Stat()
	if err != nil {
		// 对象不存在最常见的原因是客户端拿了 URL 却没传成功
		s.Log.Warn("上传对象不存在", "err", err, "key", objectKey)
		return apierr.ErrBadRequest.WithMessage("图片还没上传成功，请重试")
	}
	if info.Size > maxUploadBytes {
		return apierr.ErrUploadTooLarge
	}
	if info.Size == 0 {
		return apierr.ErrBadRequest.WithMessage("上传的图片是空的")
	}

	// 多读一个字节：正好等于 maxUploadBytes+1 说明 Stat 报的大小不可信，
	// 或者对象在上传中还在变大。LimitReader 挡住的是内存，不是信任。
	limited := io.LimitReader(obj, maxUploadBytes+1)
	img, _, err := image.Decode(limited)
	if err != nil {
		s.Log.Warn("解码上传图片失败", "err", err, "key", objectKey)
		return apierr.ErrUploadBadType
	}

	if requireFace {
		ok, err := facedetect.HasFace(img)
		if err != nil {
			s.Log.Error("人脸检测失败", "err", err, "key", objectKey)
			return apierr.ErrInternal
		}
		if !ok {
			return apierr.ErrBadRequest.WithMessage("头像需要是一张能看清正脸的照片")
		}
	}

	// 逐档生成派生图。任一档失败都当作整体失败：
	// 少一档的话前端会在某个页面拿到 404 的图，很难排查。
	for _, variant := range storage.AllVariants {
		if err := s.writeVariant(ctx, img, objectKey, variant); err != nil {
			s.Log.Error("生成派生图失败", "err", err, "key", objectKey, "variant", variant)
			return apierr.ErrInternal
		}
	}
	return nil
}

func (s *Service) writeVariant(ctx context.Context, img image.Image, objectKey, variant string) error {
	width := storage.VariantWidths[variant]

	// 不放大：原图只有 300px 宽时，写出 1080 宽的那一档只会又糊又占空间
	resized := img
	if img.Bounds().Dx() > width {
		resized = imaging.Resize(img, width, 0, imaging.Lanczos)
	}

	var buf bytes.Buffer
	if err := imaging.Encode(&buf, resized, imaging.JPEG, imaging.JPEGQuality(jpegQuality)); err != nil {
		return fmt.Errorf("编码 JPEG 失败: %w", err)
	}
	return s.Storage.Put(ctx, storage.VariantKey(objectKey, variant),
		&buf, int64(buf.Len()), "image/jpeg")
}

// removeObjects 删掉原图与全部派生图。
func (s *Service) removeObjects(ctx context.Context, objectKey string) error {
	if objectKey == "" {
		return nil
	}
	var firstErr error
	keys := append([]string{objectKey}, variantKeys(objectKey)...)
	for _, k := range keys {
		if err := s.Storage.Remove(ctx, k); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func variantKeys(objectKey string) []string {
	out := make([]string, 0, len(storage.AllVariants))
	for _, v := range storage.AllVariants {
		out = append(out, storage.VariantKey(objectKey, v))
	}
	return out
}

// ImgURL 拼图片的对外地址。空 key 返回空串，前端据此显示占位图。
func (s *Service) ImgURL(objectKey, variant string) string {
	if objectKey == "" {
		return ""
	}
	return s.Cfg.MediaBaseURL + "/img/" + storage.VariantKey(objectKey, variant)
}

func (s *Service) toPhotoView(p *model.Photo) *PhotoView {
	return &PhotoView{
		ID:        p.ID,
		Position:  p.Position,
		ObjectKey: p.ObjectKey,
		ThumbURL:  s.ImgURL(p.ObjectKey, storage.VariantThumb),
		CardURL:   s.ImgURL(p.ObjectKey, storage.VariantCard),
		FullURL:   s.ImgURL(p.ObjectKey, storage.VariantFull),
	}
}
