// Package storage 封装 MinIO 对象存储。
//
// 与云 OSS 最大的差别：MinIO 没有 URL 图片处理参数，
// 缩略图必须自己生成。这个包只负责存取，缩略图生成放在 service 层。
package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/davisu-china/tidings/backend/internal/config"
)

// 缩略图三档。会话列表用 thumb，引荐卡用 card，详情页用 full。
//
// 用 JPEG 而不是 WebP：纯 Go 没有可用的 WebP 有损编码器，
// chai2010/webp 依赖 cgo，会让镜像失去静态编译（CGO_ENABLED=0）
// 和 alpine 部署的便利。WebP 大约能再省 25% 体积，
// 但不值得为它换一套构建链路。
const (
	VariantThumb = "thumb" // 200w  会话列表、审核队列
	VariantCard  = "card"  // 800w  引荐卡
	VariantFull  = "full"  // 1080w 详情页
)

var VariantWidths = map[string]int{
	VariantThumb: 200,
	VariantCard:  800,
	VariantFull:  1080,
}

// AllVariants 保证生成顺序稳定，方便日志比对。
var AllVariants = []string{VariantThumb, VariantCard, VariantFull}

type Client struct {
	mc *minio.Client
	// pc 专门用来生成预签名 URL，它指向浏览器能访问到的公网端点。
	// 见 config.MinIOConfig.PublicEndpoint 上的说明。
	pc     *minio.Client
	bucket string
	expiry time.Duration
	log    *slog.Logger
}

func New(cfg config.MinIOConfig, log *slog.Logger) (*Client, error) {
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("初始化 minio 客户端失败: %w", err)
	}

	public := cfg.ResolvedPublicEndpoint()
	pc := mc
	if public != cfg.Endpoint {
		// 必须显式给 Region：否则签名前会先去这个地址查桶的区域，
		// 而这个地址是给浏览器用的，服务端根本连不通。
		pc, err = minio.New(public, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.PublicUseSSL,
			Region: cfg.Region,
		})
		if err != nil {
			return nil, fmt.Errorf("初始化 minio 公网签名客户端失败: %w", err)
		}
	}

	c := &Client{mc: mc, pc: pc, bucket: cfg.Bucket, expiry: cfg.PresignExpiry, log: log}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.ensureBucket(ctx, cfg.PublicRead); err != nil {
		return nil, err
	}

	log.Info("minio 已连接", "endpoint", cfg.Endpoint, "public_endpoint", public, "bucket", cfg.Bucket)
	return c, nil
}

// publicReadPolicy 只授予匿名 GetObject，**绝不包含 ListBucket**。
//
// 这条区分是整个方案成立的前提：对象 key 里带 UUID 不可猜，
// 所以单个对象可读是可接受的；但一旦允许 ListBucket，
// 任何人都能把全站用户的照片枚举下来，不可猜就不存在了。
// 改这个策略前请想清楚这一点。
const publicReadPolicy = `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {"AWS": ["*"]},
      "Action": ["s3:GetObject"],
      "Resource": ["arn:aws:s3:::%s/*"]
    }
  ]
}`

// ensureBucket 建桶并按配置设置读策略。
func (c *Client) ensureBucket(ctx context.Context, publicRead bool) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("检查 bucket 失败: %w", err)
	}
	if !exists {
		if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("创建 bucket %q 失败: %w", c.bucket, err)
		}
		c.log.Info("已创建 bucket", "bucket", c.bucket)
	}

	if !publicRead {
		// 显式收回，方便随时改回私有桶而不用手工清策略
		if err := c.mc.SetBucketPolicy(ctx, c.bucket, ""); err != nil {
			return fmt.Errorf("清除 bucket 策略失败: %w", err)
		}
		c.log.Info("bucket 保持私有", "bucket", c.bucket)
		return nil
	}

	policy := fmt.Sprintf(publicReadPolicy, c.bucket)
	if err := c.mc.SetBucketPolicy(ctx, c.bucket, policy); err != nil {
		return fmt.Errorf("设置 bucket 读策略失败: %w", err)
	}
	c.log.Info("bucket 已开匿名只读（仅 GetObject，不含 ListBucket）", "bucket", c.bucket)
	return nil
}

// NewObjectKey 由服务端生成 object key，格式 u/{uid}/{uuid}{ext}。
//
// 绝不接受前端传 key：否则用户可以构造别人的路径覆盖其头像。
func NewObjectKey(userID int64, ext string) string {
	return path.Join("u", fmt.Sprint(userID), uuid.NewString()+ext)
}

// VariantKey 返回某一档缩略图的 key，如 u/12/abc.jpg -> u/12/abc_card.jpg
func VariantKey(originalKey, variant string) string {
	ext := path.Ext(originalKey)
	return originalKey[:len(originalKey)-len(ext)] + "_" + variant + ".jpg"
}

// OwnsKey 校验 key 是否属于该用户。登记照片时必须二次校验，
// 防止前端拿到预签名 URL 后提交别人的 key。
func OwnsKey(userID int64, key string) bool {
	return len(key) > 0 && path.Dir(path.Dir(key)) == "u" &&
		path.Base(path.Dir(key)) == fmt.Sprint(userID)
}

// PresignPut 生成上传用的预签名 URL，有效期由配置决定（默认 5 分钟）。
// 用公网端点客户端签名，否则浏览器拿到的是 compose 内网地址，连不上。
func (c *Client) PresignPut(ctx context.Context, key string) (*url.URL, error) {
	u, err := c.pc.PresignedPutObject(ctx, c.bucket, key, c.expiry)
	if err != nil {
		return nil, fmt.Errorf("生成预签名 URL 失败: %w", err)
	}
	return u, nil
}

// Get 拉回对象，用于服务端生成缩略图。
func (c *Client) Get(ctx context.Context, key string) (*minio.Object, error) {
	return c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
}

// Put 写入对象。
func (c *Client) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("写入对象 %q 失败: %w", key, err)
	}
	return nil
}

// Remove 删除对象。
func (c *Client) Remove(ctx context.Context, key string) error {
	return c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
}

// Ping 供健康检查用。它比探活接口更强：顺带验证了凭证和桶是否可达。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.mc.BucketExists(ctx, c.bucket)
	return err
}
