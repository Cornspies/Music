package server

import (
	"bytes"
	"fmt"
	"image"
	"sync"
)

const maxImagePixels = 24_000_000

type pixelBudget struct {
	mu   sync.Mutex
	cond *sync.Cond
	free int64
}

func newPixelBudget(total int64) *pixelBudget {
	b := &pixelBudget{free: total}
	b.cond = sync.NewCond(&b.mu)
	return b
}

func (b *pixelBudget) acquire(n int64) {
	b.mu.Lock()
	for b.free < n {
		b.cond.Wait()
	}
	b.free -= n
	b.mu.Unlock()
}

func (b *pixelBudget) release(n int64) {
	b.mu.Lock()
	b.free += n
	b.mu.Unlock()
	b.cond.Broadcast()
}

var decodeBudget = newPixelBudget(maxImagePixels)

func decodeImage(raw []byte) (image.Image, func(), error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, nil, fmt.Errorf("empty image")
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > maxImagePixels {
		return nil, nil, fmt.Errorf("image is too large (%dx%d)", cfg.Width, cfg.Height)
	}
	decodeBudget.acquire(pixels)
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		decodeBudget.release(pixels)
		return nil, nil, err
	}
	return img, func() { decodeBudget.release(pixels) }, nil
}
