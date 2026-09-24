// Package facedetect 提供轻量的人脸检测，用于头像上传时校验「图片里是真人」。
//
// 基于 Pigo（github.com/esimov/pigo）：纯 Go、零 cgo、零外部依赖，
// 静态编译 + alpine 镜像可直接跑。内置 facefinder 级联模型。
//
// 边界：只能判断「图片里有没有人脸」，不能判断活体或本人。
// 能挡住风景/卡通/动物/无人脸网图，挡不住拿别人照片当头像。
//
// 这是入池四道门槛里唯一在 M1 就能自动执行的一道（见方案 §4.1）：
// 邮箱不验证之后，它是「账号背后是不是一个真人」的第一道也是唯一一道
// 自动检查。人工审核队列要到 M5。
package facedetect

import (
	_ "embed"
	"fmt"
	"image"

	pigo "github.com/esimov/pigo/core"
)

//go:embed facefinder
var facefinder []byte

const (
	minSize     = 20
	maxSize     = 1000
	shiftFactor = 0.1
	scaleFactor = 1.1
	// iouThreshold 是聚类的 IoU 阈值，Pigo 官方示例用 0.2
	iouThreshold = 0.2

	// 真人检测的置信度门槛。facefinder 对真人脸会在多个尺度/位置反复
	// 命中，产生大量 Q 较高、互相重叠的检测；而食物/风景/纹理这类假阳
	// 检测数量稀少、分数也低。所以同时卡「单个检测的分数」和「检测数量」
	// 两个维度：要求至少 minDetections 个 Q ≥ minScore 的检测才判有人脸。
	//
	// 阈值来自对真实数据的分位观察：
	//   - 真人脸（含缩小/模糊/压暗后的头像）通常有 7~15 个 Q ≥ 3 的检测；
	//   - 风景/物体假阳最多只有 2 个 Q ≥ 3 的检测，且多为孤立点。
	minScore      = 3.0
	minDetections = 3
)

// HasFace 判断图片里是否至少有一张人脸。
func HasFace(img image.Image) (bool, error) {
	dets, err := detectFaces(img)
	if err != nil {
		return false, err
	}
	return len(dets) > 0, nil
}

// CountFaces 返回检测到的人脸数量（按聚类去重）。
func CountFaces(img image.Image) (int, error) {
	dets, err := detectFaces(img)
	if err != nil {
		return 0, err
	}
	return len(dets), nil
}

// detectFaces 返回置信度达标的人脸检测（已聚类）。过滤逻辑见 confidentFaces。
func detectFaces(img image.Image) ([]pigo.Detection, error) {
	pixels := pigo.RgbToGrayscale(img)
	bounds := img.Bounds()
	cols, rows := bounds.Dx(), bounds.Dy()
	if cols < 20 || rows < 20 {
		return nil, fmt.Errorf("图片太小，无法检测")
	}

	classifier := pigo.NewPigo()
	cascade, err := classifier.Unpack(facefinder)
	if err != nil {
		return nil, fmt.Errorf("加载人脸模型失败: %w", err)
	}

	params := pigo.CascadeParams{
		MinSize:     minSize,
		MaxSize:     maxSize,
		ShiftFactor: shiftFactor,
		ScaleFactor: scaleFactor,
		ImageParams: pigo.ImageParams{
			Pixels: pixels,
			Rows:   rows,
			Cols:   cols,
			Dim:    cols,
		},
	}

	raw := cascade.RunCascade(params, 0.0)
	confident := confidentFaces(raw)
	if len(confident) == 0 {
		return nil, nil
	}
	return cascade.ClusterDetections(confident, iouThreshold), nil
}

// confidentFaces 从原始检测里挑出高置信度、且数量达标的候选。
//
// Pigo 的 RunCascade 只要分数 Q > 0 就算一次命中，对纹理密集的图
// （食物、风景、人群）会产生零星的低分假阳。真人脸的签名是：在同一
// 张脸上产生多个重叠的高分检测。所以这里同时卡「分数」和「数量」，
// 而不是只看有没有命中。
func confidentFaces(raw []pigo.Detection) []pigo.Detection {
	kept := make([]pigo.Detection, 0, len(raw))
	for _, d := range raw {
		if d.Q >= minScore {
			kept = append(kept, d)
		}
	}
	if len(kept) < minDetections {
		return nil
	}
	return kept
}
