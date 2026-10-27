package pricetag

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	xdraw "golang.org/x/image/draw"
)

// ocrLines runs Tesseract on img in sparse-text mode and returns its lines,
// positioned in the original tile's pixels (img is the tile scaled by scale).
func ocrLines(ctx context.Context, bin string, img image.Image, scale float64) ([]line, error) {
	out, err := tesseract(ctx, bin, img, "11", "tsv")
	if err != nil {
		return nil, err
	}
	return parseTSV(out, scale), nil
}

// ocrText runs Tesseract on img with the given page segmentation mode.
func ocrText(ctx context.Context, bin string, img image.Image, psm string) (string, error) {
	out, err := tesseract(ctx, bin, img, psm)
	return strings.Join(strings.Fields(out), " "), err
}

func tesseract(ctx context.Context, bin string, img image.Image, psm string, extra ...string) (string, error) {
	var in bytes.Buffer
	if err := png.Encode(&in, img); err != nil {
		return "", err
	}
	args := append([]string{"stdin", "stdout", "--psm", psm}, extra...)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = &in
	// Callers run several tiles at once; one thread each avoids oversubscribing.
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("tesseract: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

// parseTSV groups Tesseract's word rows (level 5) into lines.
func parseTSV(tsv string, scale float64) []line {
	type key struct{ block, par, line string }
	byKey := map[key]*line{}
	var order []key
	for row := range strings.SplitSeq(tsv, "\n") {
		f := strings.Split(row, "\t")
		if len(f) < 12 || f[0] != "5" || strings.TrimSpace(f[11]) == "" {
			continue
		}
		top, err1 := strconv.Atoi(f[7])
		height, err2 := strconv.Atoi(f[9])
		if err1 != nil || err2 != nil {
			continue
		}
		top, bottom := int(float64(top)/scale), int(float64(top+height)/scale)
		k := key{f[2], f[3], f[4]}
		l, ok := byKey[k]
		if !ok {
			l = &line{top: top, bottom: bottom}
			byKey[k] = l
			order = append(order, k)
		} else {
			l.text += " "
		}
		l.text += strings.TrimSpace(f[11])
		l.top, l.bottom = min(l.top, top), max(l.bottom, bottom)
	}
	lines := make([]line, 0, len(order))
	for _, k := range order {
		lines = append(lines, *byKey[k])
	}
	slices.SortStableFunc(lines, func(a, b line) int { return cmp.Compare(a.top, b.top) })
	return lines
}

// readBanner reads a red banner (inverted and enlarged, as one line of text)
// and then the "WHEN YOU BUY N" box to its right.
func readBanner(ctx context.Context, bin string, tile image.Image, box image.Rectangle) (banner, error) {
	text, err := ocrText(ctx, bin, scale(invert(toGray(crop(tile, box))), 2), "7")
	if err != nil {
		return banner{}, err
	}
	var b banner
	b.save, b.perLB, _ = parseSave(normalize(text))
	if b.save == 0 {
		return b, nil
	}
	side := image.Rect(box.Max.X, box.Min.Y-box.Dy()/4, box.Max.X+box.Dx()*4/5, box.Max.Y+box.Dy()/4).Intersect(tile.Bounds())
	if side.Dx() > box.Dy() {
		text, err := ocrText(ctx, bin, scale(toGray(crop(tile, side)), 2), "7")
		if err != nil {
			return banner{}, err
		}
		if m := buyRe.FindStringSubmatch(normalize(text)); m != nil {
			b.buy, _ = strconv.Atoi(m[1])
		}
	}
	return b, nil
}

// redBanners finds the wide red boxes ShopRite prints "SAVE $X" in. It
// samples a coarse grid, joins red cells into connected regions, and keeps
// regions at least 2.5 times as wide as they are tall.
func redBanners(img image.Image) []image.Rectangle {
	const step = 3
	b := img.Bounds()
	gw, gh := b.Dx()/step, b.Dy()/step
	red := make([]bool, gw*gh)
	for gy := range gh {
		for gx := range gw {
			r, g, bl, _ := img.At(b.Min.X+gx*step, b.Min.Y+gy*step).RGBA()
			r, g, bl = r>>8, g>>8, bl>>8
			red[gy*gw+gx] = r > 150 && r > g+90 && r > bl+70
		}
	}
	seen := make([]bool, len(red))
	var boxes []image.Rectangle
	for start := range red {
		if !red[start] || seen[start] {
			continue
		}
		seen[start] = true
		stack := []int{start}
		x0, y0, x1, y1, n := gw, gh, 0, 0, 0
		for len(stack) > 0 {
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			n++
			cx, cy := c%gw, c/gw
			x0, y0, x1, y1 = min(x0, cx), min(y0, cy), max(x1, cx), max(y1, cy)
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := cx+d[0], cy+d[1]
				if nx < 0 || ny < 0 || nx >= gw || ny >= gh {
					continue
				}
				if i := ny*gw + nx; red[i] && !seen[i] {
					seen[i] = true
					stack = append(stack, i)
				}
			}
		}
		w, h := (x1-x0+1)*step, (y1-y0+1)*step
		if n > 40 && w*2 > h*5 && h >= 12 {
			boxes = append(boxes, image.Rect(x0*step, y0*step, x0*step+w, y0*step+h).Add(b.Min))
		}
	}
	slices.SortFunc(boxes, func(a, b image.Rectangle) int { return cmp.Compare(a.Min.Y, b.Min.Y) })
	return boxes
}

func crop(img image.Image, r image.Rectangle) image.Image {
	if s, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	}); ok {
		return s.SubImage(r)
	}
	dst := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst
}

// toGray copies img into a grayscale image anchored at the origin.
func toGray(img image.Image) *image.Gray {
	b := img.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(g, g.Bounds(), img, b.Min, draw.Src)
	return g
}

func threshold(g *image.Gray, level uint8) *image.Gray {
	out := image.NewGray(g.Bounds())
	for i, v := range g.Pix {
		if v >= level {
			out.Pix[i] = 255
		}
	}
	return out
}

func invert(g *image.Gray) *image.Gray {
	out := image.NewGray(g.Bounds())
	for i, v := range g.Pix {
		out.Pix[i] = 255 - v
	}
	return out
}

func scale(g *image.Gray, f float64) *image.Gray {
	b := g.Bounds()
	out := image.NewGray(image.Rect(0, 0, max(1, int(float64(b.Dx())*f)), max(1, int(float64(b.Dy())*f))))
	xdraw.CatmullRom.Scale(out, out.Bounds(), g, b, xdraw.Src, nil)
	return out
}
