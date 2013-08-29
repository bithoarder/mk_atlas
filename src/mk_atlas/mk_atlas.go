package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
)

//////////////////////////////////////////////////////////////////////////////

func ImageMaxAlpha(rawimage image.Image) uint32 {
	b := rawimage.Bounds()
	var maxAlpha uint32 = 0

	switch i := rawimage.(type) {
	case *image.RGBA:
		//fmt.Printf("ImageMaxAlpha %p %#v\n", i.Pix, i.Bounds())
		for y := 0; y < b.Dy(); y++ {
			for x := 0; x < b.Dx(); x++ {
				//fmt.Println(x, y)
				a := uint32(i.Pix[(x*4+y*i.Stride)+3])
				if a > maxAlpha {
					maxAlpha = a
				}
			}
		}
	default:
		panic("implement fallback")
	}

	return maxAlpha
}

func TrimImage(src *image.RGBA) (dst *image.RGBA) {
	trim := src.Bounds()

	//fmt.Printf("TrimImage %p %#v\n", src.Pix, src.Bounds())

	for trim.Max.X-trim.Min.X > 1 && ImageMaxAlpha(src.SubImage(image.Rect(trim.Max.X-1, trim.Min.Y, trim.Max.X, trim.Max.Y))) == 0 {
		trim.Max.X -= 1
	}
	for trim.Max.X-trim.Min.X > 1 && ImageMaxAlpha(src.SubImage(image.Rect(trim.Min.X, trim.Min.Y, trim.Min.X+1, trim.Max.Y))) == 0 {
		trim.Min.X += 1
	}

	for trim.Max.Y-trim.Min.Y > 1 && ImageMaxAlpha(src.SubImage(image.Rect(trim.Min.X, trim.Max.Y-1, trim.Max.X, trim.Max.Y))) == 0 {
		trim.Max.Y -= 1
	}
	for trim.Max.Y-trim.Min.Y > 1 && ImageMaxAlpha(src.SubImage(image.Rect(trim.Min.X, trim.Min.Y, trim.Max.X, trim.Min.Y+1))) == 0 {
		trim.Min.Y += 1
	}

	return src.SubImage(trim).(*image.RGBA)
}

///////////////////////////////////////////////////////////////////////////////

type AtlasImage struct {
	Path      string
	OrgBounds image.Rectangle
	Image     *image.RGBA
	AtlasPos  image.Point
}

func (i *AtlasImage) PixelArea() int {
	return i.Image.Bounds().Dx() * i.Image.Bounds().Dy()
}

func (i *AtlasImage) ManhattenSize() int {
	return i.Image.Bounds().Dx() + i.Image.Bounds().Dy()
}

///////////////////////////////////////////////////////////////////////////////

type AtlasImageAreaSorter []AtlasImage

func (i AtlasImageAreaSorter) Len() int {
	return len(i)
}

func (i AtlasImageAreaSorter) Less(a, b int) bool {
	//return i[a].PixelArea() < i[b].PixelArea()
	return i[a].ManhattenSize() < i[b].ManhattenSize()
}

func (i AtlasImageAreaSorter) Swap(a, b int) {
	i[a], i[b] = i[b], i[a]
}

///////////////////////////////////////////////////////////////////////////////

type Atlas struct {
	Images []AtlasImage
}

func NewAtlas() *Atlas {
	return &Atlas{}
}

func (a *Atlas) AddImage(path string) (err error) {
	img := AtlasImage{Path: path}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	rawimg, _, err := image.Decode(f)
	if err != nil {
		return err
	}

	img.OrgBounds = rawimg.Bounds()
	img.Image = image.NewRGBA(rawimg.Bounds())
	draw.Draw(img.Image, img.Image.Bounds(), rawimg, image.ZP, draw.Over)

	img.Image = TrimImage(img.Image)

	fmt.Printf("%dx%d -> %dx%d : %s\n", img.OrgBounds.Dx(), img.OrgBounds.Dy(), img.Image.Bounds().Dx(), img.Image.Bounds().Dy(), path)

	a.Images = append(a.Images, img)

	return nil
}

func (a *Atlas) AddImages(pattern string) (err error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}

	for _, match := range matches {
		err = a.AddImage(match)
		if err != nil {
			return err
		}
	}

	return nil
}

type node struct {
	Left, Right *node
	Rect        image.Rectangle
	Used        bool
}

type SimpleRand struct {
	Seed uint32
}

func (r *SimpleRand) Get() int {
	r.Seed = (r.Seed << 1) + 1
	if int32(r.Seed) < 0 {
		r.Seed ^= 0x88888eef
	}
	return int(r.Seed)
}

// http://www.blackpawn.com/texts/lightmaps/default.html
func (n *node) Insert(size image.Point, rnd *SimpleRand) (r *image.Rectangle) {
	//fmt.Printf("Insert %#v %#v %#v\n", size, n.Rect, n.Used)
	if n.Left != nil {
		if rnd.Get() < 0x40000000 {
			r = n.Left.Insert(size, rnd)
			if r == nil {
				r = n.Right.Insert(size, rnd)
			}
		} else {
			r = n.Right.Insert(size, rnd)
			if r == nil {
				r = n.Left.Insert(size, rnd)
			}
		}
	} else {
		if !n.Used {
			ds := n.Rect.Size().Sub(size)
			if ds.X >= 0 && ds.Y >= 0 {
				n.Used = true
				if ds.X == 0 && ds.Y == 0 {
					//r = image.Rect(n.Rect.Min.X, n.Rect.Min.Y, n.Rect.Min.X+size.X, n.Rect.Min.Y+size.Y)
					r = &n.Rect
				} else {
					if ds.X >= ds.Y {
						n.Left = &node{Rect: image.Rect(n.Rect.Min.X, n.Rect.Min.Y, n.Rect.Min.X+size.X, n.Rect.Max.Y)}
						n.Right = &node{Rect: image.Rect(n.Rect.Min.X+size.X, n.Rect.Min.Y, n.Rect.Max.X, n.Rect.Max.Y)}
					} else {
						n.Left = &node{Rect: image.Rect(n.Rect.Min.X, n.Rect.Min.Y, n.Rect.Max.X, n.Rect.Min.Y+size.Y)}
						n.Right = &node{Rect: image.Rect(n.Rect.Min.X, n.Rect.Min.Y+size.Y, n.Rect.Max.X, n.Rect.Max.Y)}
					}
					r = n.Left.Insert(size, rnd)
				}
			}
		}
	}
	return
}

func (n *node) Score() (score int64) {
	if !n.Used {
		score += int64(n.Rect.Dx()*n.Rect.Dy()) * int64(n.Rect.Dx()*n.Rect.Dy())
	}
	if n.Left != nil {
		score += n.Left.Score()
	}
	if n.Right != nil {
		score += n.Right.Score()
	}
	return
}

func (a *Atlas) PackImages(atlasSize image.Point, rnd *SimpleRand) (score int64) {
	root := node{Rect: image.Rect(1, 1, atlasSize.X, atlasSize.Y)}

	for i := range a.Images {
		img := &a.Images[i]
		r := root.Insert(img.Image.Bounds().Size().Add(image.Pt(1, 1)), rnd)
		//fmt.Printf("%dx%d @ %d,%d : %s\n", img.Image.Bounds().Dx(), img.Image.Bounds().Dy(), r.Min.X, r.Min.Y, img.Path)
		if r == nil {
			return -1
		}
		img.AtlasPos = r.Min
	}

	return root.Score()
}

func (a *Atlas) PackImages2(atlasSize image.Point) (err error) {
	sort.Sort(sort.Reverse(AtlasImageAreaSorter(a.Images)))

	var bestScore int64
	var bestSeed uint32

	for i := 0; i < 25000; i++ {
		score := a.PackImages(atlasSize, &SimpleRand{uint32(i)})
		if score < 0 {
			//fmt.Printf("%d: Failed to fit all images\n", i)
		} else {
			if score > bestScore {
				fmt.Printf("%d: %d\n", i, score)
				bestScore = score
				bestSeed = uint32(i)
			}
		}
	}

	if bestSeed == 0 {
		return fmt.Errorf("Failed to fit all images")
	}

	if a.PackImages(atlasSize, &SimpleRand{bestSeed}) != bestScore {
		return fmt.Errorf("packing was not deterministic!")
	}

	return nil
}

func (a *Atlas) SaveAtlasImage(path string, atlasSize image.Point) (err error) {
	dstimg := image.NewRGBA(image.Rect(0, 0, atlasSize.X, atlasSize.Y))

	// fill with solid color
	for y := 0; y < atlasSize.Y; y++ {
		for x := 0; x < atlasSize.X; x++ {
			dstimg.Pix[x*4+y*dstimg.Stride+0] = 0
			dstimg.Pix[x*4+y*dstimg.Stride+1] = 0
			dstimg.Pix[x*4+y*dstimg.Stride+2] = 128
			dstimg.Pix[x*4+y*dstimg.Stride+3] = 255
		}
	}

	for i := range a.Images {
		img := a.Images[i]
		//fmt.Println(img.AtlasPos)
		draw.Draw(dstimg,
			//img.Image.Rect.Add(img.AtlasPos),
			image.Rect(img.AtlasPos.X, img.AtlasPos.Y, img.AtlasPos.X+img.Image.Rect.Dx(), img.AtlasPos.Y+img.Image.Rect.Dy()),
			img.Image, img.Image.Rect.Min, draw.Src)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	err = png.Encode(f, dstimg)
	if err != nil {
		return err
	}

	return nil
}

///////////////////////////////////////////////////////////////////////////////

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")

func main() {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			panic(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	atlas := NewAtlas()

	for _, arg := range flag.Args() {
		err := atlas.AddImages(arg)
		if err != nil {
			panic(err)
		}
	}

	altasSize := image.Pt(1024, 1024)
	err := atlas.PackImages2(altasSize)
	if err != nil {
		panic(err)
	}

	err = atlas.SaveAtlasImage("/tmp/test.png", altasSize)
	if err != nil {
		panic(err)
	}
}

///////////////////////////////////////////////////////////////////////////////
