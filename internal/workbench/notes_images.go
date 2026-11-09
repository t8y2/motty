package workbench

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

var hitNoteLoadImages = tree.ID(notesHits, "load-images")
var noteImagePattern = regexp.MustCompile(`!\[[^\]]*\]\((https?://[^\s)]+)\)`)

type noteImage struct {
	url    string
	bitmap gfx.Bitmap
}

func noteImageURLs(body string) []string {
	var urls []string
	seen := map[string]bool{}
	fenced := false
	for _, line := range strings.Split(body, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "```") || strings.HasPrefix(trim, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		for _, m := range noteImagePattern.FindAllStringSubmatch(line, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				urls = append(urls, m[1])
			}
		}
	}
	return urls
}
func (s *State) cancelNoteImages() {
	s.notes.imageToken++
	if s.notes.imageCancel != nil {
		s.notes.imageCancel()
		s.notes.imageCancel = nil
	}
	s.notes.images = nil
	s.notes.imageSource = ""
}
func (s *State) loadNoteImages() {
	s.loadNoteImagesContent(&s.notes, false)
}
func (s *State) loadNoteImagesContent(n *notesUI, side bool) {
	s.loadNoteImagesTarget(n, func(owner *State) *notesUI {
		if side {
			if owner.notes.sidebar == nil {
				return nil
			}
			owner.syncNoteSidebar()
			return &owner.notes.sidebar.content
		}
		return &owner.notes
	}, s.options.Invalidate)
}

func (s *State) loadNoteImagesTarget(n *notesUI, resolve func(*State) *notesUI, invalidate func()) {
	n.imageToken++
	if n.imageCancel != nil {
		n.imageCancel()
	}
	n.images = nil
	n.imageSource = ""
	token := n.imageToken
	source := n.body.String()
	urls := noteImageURLs(source)
	if len(urls) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	n.imageCancel = cancel
	n.imageSource = source
	if len(urls) > 8 {
		n.error = s.noteText("Loading the first eight images.", "加载前八张图片。")
		urls = urls[:8]
	}
	go func() {
		defer cancel()
		for _, url := range urls {
			bitmap, err := fetchNoteImage(ctx, url)
			if ctx.Err() != nil {
				return
			}
			s.aiPostWithInvalidator(func(owner *State) {
				current := resolve(owner)
				if current == nil {
					return
				}
				if owner.closed || current.imageToken != token || current.imageSource != source || current.body.String() != source {
					return
				}
				if err != nil {
					current.error = err.Error()
					return
				}
				current.images = append(current.images, noteImage{url, bitmap})
			}, invalidate)
		}
	}()
}
func fetchNoteImage(ctx context.Context, url string) (gfx.Bitmap, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return gfx.Bitmap{}, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return gfx.Bitmap{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return gfx.Bitmap{}, fmt.Errorf("image: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return gfx.Bitmap{}, err
	}
	if len(data) > 8<<20 {
		return gfx.Bitmap{}, fmt.Errorf("image exceeds 8 MiB")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return gfx.Bitmap{}, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16<<20 {
		return gfx.Bitmap{}, fmt.Errorf("image dimensions exceed limit")
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return gfx.Bitmap{}, err
	}
	rgba := image.NewRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(rgba, rgba.Bounds(), im, im.Bounds().Min, draw.Src)
	hash := noteHash(url + string(data))
	id := binary.LittleEndian.Uint64([]byte(hash)[:8])
	return gfx.Bitmap{Pix: rgba.Pix, Width: config.Width, Height: config.Height, Stride: rgba.Stride, ID: id, Version: 1}, nil
}
func (s *State) paintNoteImagesContent(c *gfx.Canvas, b gfx.Rect, y float32, n *notesUI) float32 {
	if n.imageSource != n.body.String() {
		return y
	}
	for _, im := range n.images {
		height := min(float32(320), b.Width*float32(im.bitmap.Height)/float32(im.bitmap.Width))
		components.PaintImageView(c, s.Theme(), gfx.Rect{X: b.X, Y: y, Width: b.Width, Height: height}, components.ImageViewOptions{Bitmap: im.bitmap})
		y += height + 12
	}
	return y
}
