package facedetect

import (
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/disintegration/imaging"
	pigo "github.com/esimov/pigo/core"
)

// 人脸检测是 M1 入池四道门槛里唯一能自动执行的一道（见 §4.1）：
// 邮箱不验证之后，「账号背后是不是一个真人」全靠它。
// 它的两个错误方向代价不对称 —— 漏放一张没人脸的头像可以靠人工巡检兜住，
// 但把真实用户的正常头像误杀，用户在建档第一步就走不下去。
// 下面的用例两边都钉住了。

func loadImage(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开图片失败: %v", err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatalf("解码图片失败: %v", err)
	}
	return img
}

// sample.jpg 是 pigo 官方 testdata 里的人脸图。
func TestHasFaceOnSample(t *testing.T) {
	has, err := HasFace(loadImage(t, "testdata/sample.jpg"))
	if err != nil {
		t.Fatalf("检测失败: %v", err)
	}
	if !has {
		t.Error("sample.jpg 应检测到人脸，却返回了 false")
	}
}

func TestNoFaceOnSolidColor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 200; x++ {
			img.Set(x, y, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}
	has, err := HasFace(img)
	if err != nil {
		t.Fatalf("检测失败: %v", err)
	}
	if has {
		t.Error("纯色图不应检测到人脸")
	}
}

func TestTooSmallImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 5, 5))
	if _, err := HasFace(img); err == nil {
		t.Error("过小的图片应返回错误，而不是静默判为无人脸")
	}
}

// TestHasFaceOnDownscaledSample 防的是误杀：上传的头像会被压到
// 800px 以内再进来，缩小后仍必须认得出来。
func TestHasFaceOnDownscaledSample(t *testing.T) {
	small := imaging.Resize(loadImage(t, "testdata/sample.jpg"), 60, 0, imaging.Lanczos)
	has, err := HasFace(small)
	if err != nil {
		t.Fatalf("检测失败: %v", err)
	}
	if !has {
		t.Error("缩小到 60px 的人脸仍应被检测到")
	}
}

// TestConfidentFaces 钉住阈值逻辑：真人脸有大量高置信度检测，
// 食物/风景/纹理这类假阳只有零星低分检测，必须被滤掉。
func TestConfidentFaces(t *testing.T) {
	real := confidentFaces([]pigo.Detection{
		{Q: 12.0}, {Q: 8.0}, {Q: 5.5}, {Q: 3.0}, {Q: 1.0},
	})
	if len(real) != 4 {
		t.Errorf("真人脸应保留 4 个高置信检测，得到 %d", len(real))
	}

	if got := confidentFaces([]pigo.Detection{{Q: 5.0}, {Q: 0.5}, {Q: 1.0}}); got != nil {
		t.Error("零星高置信 + 低分混杂应判为无人脸")
	}
	if got := confidentFaces([]pigo.Detection{{Q: 2.9}, {Q: 0.1}, {Q: 1.2}}); got != nil {
		t.Error("全低分应判为无人脸")
	}
	if got := confidentFaces(nil); got != nil {
		t.Error("空输入应判为无人脸")
	}
}
