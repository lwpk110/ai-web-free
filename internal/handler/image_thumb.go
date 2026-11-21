package handler

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

var thumbBuckets = []int{320, 480, 720, 960}

// thumbEncodeSem 限制并发图片缩略图缩放与 JPEG 编码数量，防止突发请求挤占 CPU/内存。
var thumbEncodeSem = make(chan struct{}, 4)

var (
	thumbBuildMu   sync.Mutex
	thumbInFlight  = make(map[string]*sync.Mutex)
)

func lockThumbKey(key string) func() {
	thumbBuildMu.Lock()
	mu, ok := thumbInFlight[key]
	if !ok {
		mu = &sync.Mutex{}
		thumbInFlight[key] = mu
	}
	thumbBuildMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func snapThumbBucket(requestedWidth int) int {
	if requestedWidth <= 0 {
		return 0
	}
	for _, b := range thumbBuckets {
		if requestedWidth <= b {
			return b
		}
	}
	return thumbBuckets[len(thumbBuckets)-1]
}

// resizeBilinearNRGBA 使用纯 Go 双线性插值将图片等比缩小到目标最大宽度，零 CGO 依赖。
func resizeBilinearNRGBA(src image.Image, targetWidth int) image.Image {
	b := src.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	if srcW <= 0 || srcH <= 0 || targetWidth <= 0 || srcW <= targetWidth {
		return src
	}

	targetHeight := (srcH * targetWidth) / srcW
	if targetHeight < 1 {
		targetHeight = 1
	}

	// 将任意源色彩空间统一转换为 NRGBA 便于快速直接像素访问
	nrgba, ok := src.(*image.NRGBA)
	if !ok || nrgba.Rect.Min.X != 0 || nrgba.Rect.Min.Y != 0 {
		converted := image.NewNRGBA(image.Rect(0, 0, srcW, srcH))
		draw.Draw(converted, converted.Bounds(), src, b.Min, draw.Src)
		nrgba = converted
	}

	dst := image.NewNRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	srcStride := nrgba.Stride
	dstStride := dst.Stride
	srcPix := nrgba.Pix
	dstPix := dst.Pix

	scaleX := float64(srcW) / float64(targetWidth)
	scaleY := float64(srcH) / float64(targetHeight)

	for y := 0; y < targetHeight; y++ {
		sy := (float64(y)+0.5)*scaleY - 0.5
		if sy < 0 {
			sy = 0
		}
		y0 := int(sy)
		y1 := y0 + 1
		if y1 >= srcH {
			y1 = srcH - 1
		}
		fy := sy - float64(y0)
		wy0 := 1.0 - fy
		wy1 := fy

		row0Off := y0 * srcStride
		row1Off := y1 * srcStride
		dstRowOff := y * dstStride

		for x := 0; x < targetWidth; x++ {
			sx := (float64(x)+0.5)*scaleX - 0.5
			if sx < 0 {
				sx = 0
			}
			x0 := int(sx)
			x1 := x0 + 1
			if x1 >= srcW {
				x1 = srcW - 1
			}
			fx := sx - float64(x0)
			wx0 := 1.0 - fx
			wx1 := fx

			w00 := wx0 * wy0
			w10 := wx1 * wy0
			w01 := wx0 * wy1
			w11 := wx1 * wy1

			p00 := row0Off + x0*4
			p10 := row0Off + x1*4
			p01 := row1Off + x0*4
			p11 := row1Off + x1*4

			r := w00*float64(srcPix[p00]) + w10*float64(srcPix[p10]) + w01*float64(srcPix[p01]) + w11*float64(srcPix[p11])
			g := w00*float64(srcPix[p00+1]) + w10*float64(srcPix[p10+1]) + w01*float64(srcPix[p01+1]) + w11*float64(srcPix[p11+1])
			bl := w00*float64(srcPix[p00+2]) + w10*float64(srcPix[p10+2]) + w01*float64(srcPix[p01+2]) + w11*float64(srcPix[p11+2])
			a := w00*float64(srcPix[p00+3]) + w10*float64(srcPix[p10+3]) + w01*float64(srcPix[p01+3]) + w11*float64(srcPix[p11+3])

			dp := dstRowOff + x*4
			// 将半透明区域预合成为白底，避免 PNG 透明像素转 JPEG 后变成黑色
			if a < 254.5 {
				alpha := a / 255.0
				inv := (1.0 - alpha) * 255.0
				r = r*alpha + inv
				g = g*alpha + inv
				bl = bl*alpha + inv
			}
			dstPix[dp] = clampUint8(r)
			dstPix[dp+1] = clampUint8(g)
			dstPix[dp+2] = clampUint8(bl)
			dstPix[dp+3] = 255
		}
	}

	return dst
}

func clampUint8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func encodeJPEGThumbBytes(raw []byte, targetWidth int) ([]byte, bool) {
	if len(raw) == 0 || targetWidth <= 0 {
		return nil, false
	}
	// 已经极小（< 16KB）的图片无需重复转码
	if len(raw) < 16*1024 {
		return nil, false
	}

	thumbEncodeSem <- struct{}{}
	defer func() { <-thumbEncodeSem }()

	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	_ = gif.Decode // 确保 gif 解码器保留引用
	_ = png.Decode

	srcW := img.Bounds().Dx()
	if srcW <= targetWidth && strings.EqualFold(format, "jpeg") && len(raw) < 220*1024 {
		return nil, false
	}

	resized := resizeBilinearNRGBA(img, targetWidth)
	// 若图片为包含透明度的 PNG 但未经过 resizeBilinearNRGBA（srcW <= targetWidth），合成白底转为紧凑 JPEG
	if resized == img && format == "png" {
		b := img.Bounds()
		canvas := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
		draw.Draw(canvas, canvas.Bounds(), img, b.Min, draw.Over)
		resized = canvas
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, resized, &jpeg.Options{Quality: 82}); err != nil {
		return nil, false
	}
	if out.Len() == 0 || out.Len() >= len(raw) {
		return nil, false
	}
	return out.Bytes(), true
}

func parseRequestedThumbWidth(raw string) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	w, err := strconv.Atoi(trimmed)
	if err != nil || w <= 0 {
		return 0
	}
	return snapThumbBucket(w)
}

// serveStaticImageThumb 按需生成并磁盘缓存 ?w=<bucket> 缩略图，命中 L1 thumbs 缓存时走 http.ServeContent 零额外编码。
func (h *GalleryHandler) serveStaticImageThumb(c *gin.Context, filename string, bucket int, cacheCtl string) bool {
	if bucket <= 0 {
		return false
	}
	relPath := "images/" + filename

	// 1. 若有本地磁盘路径，优先检查/生成 L1 缩略图文件缓存（data/storage/thumbs/<filename>.w<bucket>.jpg）
	if lpr, ok := h.storageEngine.(storage.LocalPathResolver); ok && lpr != nil {
		if origAbs, exists := lpr.LocalFilePath(relPath); exists {
			thumbsDir := filepath.Join(filepath.Dir(filepath.Dir(origAbs)), "thumbs")
			thumbAbs := filepath.Join(thumbsDir, fmt.Sprintf("%s.w%d.jpg", filename, bucket))

			if info, err := os.Stat(thumbAbs); err == nil && !info.IsDir() && info.Size() > 0 {
				if serveOpenedLocalMediaFile(c, thumbAbs, filepath.Base(thumbAbs), "image/jpeg", cacheCtl, info) {
					return true
				}
			}

			unlock := lockThumbKey(thumbAbs)
			defer unlock()
			// 二次检查，防止并发请求重复生成
			if info, err := os.Stat(thumbAbs); err == nil && !info.IsDir() && info.Size() > 0 {
				if serveOpenedLocalMediaFile(c, thumbAbs, filepath.Base(thumbAbs), "image/jpeg", cacheCtl, info) {
					return true
				}
			}

			raw, err := os.ReadFile(origAbs)
			if err != nil {
				return false
			}
			thumbBytes, ok := encodeJPEGThumbBytes(raw, bucket)
			if !ok {
				return false
			}
			_ = os.MkdirAll(thumbsDir, 0o755)
			tmpPath := fmt.Sprintf("%s.tmp.%d", thumbAbs, time.Now().UnixNano())
			if writeErr := os.WriteFile(tmpPath, thumbBytes, 0o644); writeErr == nil {
				if renameErr := os.Rename(tmpPath, thumbAbs); renameErr == nil {
					if info, statErr := os.Stat(thumbAbs); statErr == nil {
						if serveOpenedLocalMediaFile(c, thumbAbs, filepath.Base(thumbAbs), "image/jpeg", cacheCtl, info) {
							return true
						}
					}
				} else {
					_ = os.Remove(tmpPath)
				}
			}
			serveRangeMediaBytes(c, filename+".jpg", "image/jpeg", cacheCtl, time.Now(), thumbBytes)
			return true
		}
	}

	// 2. 统一存储引擎（如内存/Tiered L2 回填）按需缩放返回
	if mse, ok := h.storageEngine.(storage.MediaStorageEngine); ok && mse != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		data, err := mse.Read(ctx, relPath)
		cancel()
		if err == nil && len(data) > 0 {
			if thumbBytes, encoded := encodeJPEGThumbBytes(data, bucket); encoded {
				serveRangeMediaBytes(c, filename+".jpg", "image/jpeg", cacheCtl, time.Time{}, thumbBytes)
				return true
			}
			serveRangeMediaBytes(c, filename, detectMediaMimeFromExt(filepath.Ext(filename), data), cacheCtl, time.Time{}, data)
			return true
		}
	}

	return false
}

func serveOpenedLocalMediaFile(c *gin.Context, absPath, name, mime, cacheCtl string, info os.FileInfo) bool {
	f, err := os.Open(absPath)
	if err != nil {
		return false
	}
	defer f.Close()
	c.Header("Content-Type", mime)
	c.Header("Cache-Control", cacheCtl)
	c.Header("Accept-Ranges", "bytes")
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), f)
	return true
}
