package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/davisu-china/tidings/backend/internal/model"
	"github.com/davisu-china/tidings/backend/internal/pkg/apierr"
	"github.com/davisu-china/tidings/backend/internal/pkg/password"
)

// 重置长度下限。比注册门槛高一位 —— 重置密码是运营口头或工单传递的，
// 中间过手的人多，短密码在这里比在注册表单上危险得多。
const resetPasswordMinLen = 10

// ResetPassword 是 MVP 唯一的找回密码途径。
//
// 没有邮箱验证、没有短信验证码，所以「忘记密码」只能由运营在后台重置。
// 这不是没做完，是这一版明确接受的取舍：唯一的自动通道是 Web Push，
// 而 Web Push 是设备级的，换设备就没了，本来也当不了找回凭据。
//
// 三件事必须在同一个事务里：
//  1. 换掉密码哈希
//  2. token_version + 1 —— 让该用户所有已签发的 access / refresh 立即失效。
//     只改密码不递增的话，拿到旧 token 的人（包括盗号者）还能继续用，
//     改密码就等于没改。
//  3. 记 admin_actions —— 人工操作必须可追溯，否则误操作无法复盘
func (s *Service) ResetPassword(ctx context.Context, operator string, userID int64, newPassword, reason string) error {
	if operator == "" {
		return apierr.ErrBadRequest.WithMessage("缺少操作人")
	}
	if err := password.Validate(newPassword, resetPasswordMinLen); err != nil {
		return resetPasswordError(err)
	}

	hash, err := password.Hash(newPassword, s.Cfg.Auth.BcryptCost)
	if err != nil {
		s.Log.Error("生成密码哈希失败", "err", err, "uid", userID)
		return apierr.ErrInternal
	}

	err = s.Repo.Tx(func(tx *gorm.DB) error {
		res := tx.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
			"password_hash": hash,
			"token_version": gorm.Expr("token_version + 1"),
		})
		if res.Error != nil {
			return res.Error
		}
		// 影响行数为 0 表示用户不存在。放在事务里判断，
		// 避免「查一次再改一次」中间被删掉。
		if res.RowsAffected == 0 {
			return apierr.ErrNotFound.WithMessage("用户不存在")
		}
		return tx.Create(&model.AdminAction{
			Operator:     operator,
			TargetUserID: userID,
			Action:       model.ActionResetPassword,
			Reason:       reason,
		}).Error
	})
	if err != nil {
		var apiErr *apierr.Error
		if errors.As(err, &apiErr) {
			return apiErr
		}
		s.Log.Error("重置密码失败", "err", err, "uid", userID, "operator", operator)
		return apierr.ErrInternal
	}

	// 日志里写操作人和目标用户，不写密码本身
	s.Log.Warn("运营重置了用户密码", "uid", userID, "operator", operator, "reason", reason)
	return nil
}

func resetPasswordError(err error) error {
	switch {
	case errors.Is(err, password.ErrTooLong):
		return apierr.ErrPasswordTooLong
	case errors.Is(err, password.ErrEmpty):
		return apierr.ErrWeakPassword.WithMessage("请填写新密码")
	default:
		return apierr.ErrWeakPassword.WithMessage("新密码至少 10 位")
	}
}
