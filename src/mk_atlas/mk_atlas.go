package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"text/template"
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

	for i := 0; i < 1000; i++ {
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

func (a *Atlas) SaveAtlasImage(path string, atlasSize image.Point, drawpadding bool) (err error) {
	dstimg := image.NewRGBA(image.Rect(0, 0, atlasSize.X, atlasSize.Y))

	// fill with solid color
	for y := 0; y < atlasSize.Y; y++ {
		for x := 0; x < atlasSize.X; x++ {
			dstimg.Pix[x*4+y*dstimg.Stride+0] = 0
			dstimg.Pix[x*4+y*dstimg.Stride+1] = 0
			dstimg.Pix[x*4+y*dstimg.Stride+2] = 0
			dstimg.Pix[x*4+y*dstimg.Stride+3] = 0
		}
	}

	for i := range a.Images {
		img := a.Images[i]
		dstrect := image.Rect(img.AtlasPos.X, img.AtlasPos.Y, img.AtlasPos.X+img.Image.Rect.Dx(), img.AtlasPos.Y+img.Image.Rect.Dy())
		if drawpadding {
			draw.Draw(dstimg, dstrect.Inset(-1), image.NewUniform(color.RGBA{0, 0, 0, 255}), image.ZP, draw.Src)
		}
		draw.Draw(dstimg, dstrect, img.Image, img.Image.Rect.Min, draw.Src)
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

type Dimension struct {
	Width, Height int
}

type Point struct {
	X, Y int
}

type ImageMeta struct {
	Position     Point
	Size         Dimension
	OriginalSize Dimension
	Offset       Point
}

type AtlasMeta struct {
	Size   Dimension
	Images map[string]ImageMeta
}

func (a *Atlas) AtlasMeta(strip int, atlasSize image.Point) (meta AtlasMeta) {
	meta = AtlasMeta{Size: Dimension{atlasSize.X, atlasSize.Y}, Images: make(map[string]ImageMeta)}

	for _, img := range a.Images {
		path := filepath.Join(strings.Split(img.Path, string(filepath.Separator))[strip:]...)
		meta.Images[path] = ImageMeta{
			Position:     Point{img.AtlasPos.X, img.AtlasPos.Y},
			Size:         Dimension{img.Image.Bounds().Dx(), img.Image.Bounds().Dy()},
			OriginalSize: Dimension{img.OrgBounds.Dx(), img.OrgBounds.Dy()},
			Offset:       Point{img.Image.Bounds().Min.X, img.Image.Bounds().Min.Y},
		}
	}

	return
}

///////////////////////////////////////////////////////////////////////////////

func (a *Atlas) SaveAtlasMeta(path string, strip int, atlasSize image.Point) (err error) {
	meta := a.AtlasMeta(strip, atlasSize)

	var f *os.File
	if f, err = os.Create(path); err == nil {
		defer f.Close()
		var body []byte
		if body, err = json.MarshalIndent(meta, "", "  "); err == nil {
			_, err = f.Write(body)
		}
	}

	return err
}

///////////////////////////////////////////////////////////////////////////////

func PathAsASVarName(path string) string {
	//r := []rune(filepath.Base(path))
	r := []rune(path)
	for i := 0; i < len(r); i++ {
		if !((r[i] >= 'A' && r[i] <= 'Z') || (r[i] >= 'a' && r[i] <= 'z') || (r[i] >= '0' && r[i] <= '9')) {
			r[i] = '_'
		}
	}
	return string(r)
}

func CleanASPath(path string) string {
	return strings.Replace(path, "\\", "/", -1)
}

var actionScriptTemplate = template.Must(template.New("as3_template").Funcs(template.FuncMap{"PathAsASVarName": PathAsASVarName, "CleanASPath": CleanASPath}).Parse(`
{{$meta:=.meta}}
package {{.package}}
{
	public class {{.name}}
	{
		public static const width:uint = {{.meta.Size.Width}};
		public static const height:uint = {{.meta.Size.Height}};

		{{range $path,$image := .meta.Images}}
		public static const {{PathAsASVarName $path}}:AtlasImageMeta = new AtlasImageMeta({{/*
			*/}}{{$image.Position.X}}, {{$image.Position.Y}}, {{/*
			*/}}{{$image.Size.Width}}, {{$image.Size.Height}}, {{/*
			*/}}{{$image.OriginalSize.Width}}, {{$image.OriginalSize.Height}}, {{/*
			*/}}{{$image.Offset.X}}, {{$image.Offset.Y}}, {{/*
			*/}}{{$image.Position.X}}.0/{{$meta.Size.Width}}.0, {{$image.Position.Y}}.0/{{$meta.Size.Height}}.0, {{/*
			*/}}({{$image.Position.X}}.0+{{$image.Size.Width}}.0)/{{$meta.Size.Width}}.0, ({{$image.Position.Y}}.0+{{$image.Size.Height}}.0)/{{$meta.Size.Height}}.0 {{/*
			*/}});{{end}}

		public static const images:Object = {
			{{range $path,$image := .meta.Images}}
			"{{CleanASPath $path}}": {{PathAsASVarName $path}},{{end}}
			"dummy": null // find a more elegant way to fix the trailing ,
		};
	}
}
`))

var actionScriptMetaClassTemplate = template.Must(template.New("as3_meta_template").Parse(`
package {{.package}}
{
	public class AtlasImageMeta
	{
		public var x:uint, y:uint, width:uint, height:uint, orgwidth:uint, orgheight:uint, offx:uint, offy:uint;
		public var u0:Number, v0:Number, u1:Number, v1:Number;
		public function AtlasImageMeta(x:uint, y:uint, width:uint, height:uint, orgwidth:uint, orgheight:uint, offx:uint, offy:uint, u0:Number, v0:Number, u1:Number, v1:Number)
		{
			this.x = x;
			this.y = y;
			this.width = width;
			this.height = height;
			this.orgwidth = orgwidth;
			this.orgheight = orgheight;
			this.offx = offx;
			this.offy = offy;
			this.u0 = u0;
			this.v0 = v0;
			this.u1 = u1;
			this.v1 = v1;
		}
	}
}
`))

func (a *Atlas) SaveAtlasMetaAsActionScript(path string, name string, strip int, atlasSize image.Point) (err error) {
	meta := a.AtlasMeta(strip, atlasSize)

	// todo: escape " and \ in path keys.

	nameparts := strings.Split(name, ".")

	var f *os.File
	if f, err = os.Create(path); err == nil {
		defer f.Close()
		err = actionScriptTemplate.Execute(f, map[string]interface{}{
			"package": strings.Join(nameparts[:len(nameparts)-1], "."),
			"name":    nameparts[len(nameparts)-1],
			"meta":    meta,
		})
	}

	if err == nil {
		if f, err = os.Create(filepath.Join(filepath.Dir(path), "AtlasImageMeta.as")); err == nil {
			defer f.Close()
			err = actionScriptMetaClassTemplate.Execute(f, map[string]interface{}{
				"package": strings.Join(nameparts[:len(nameparts)-1], "."),
				"meta":    meta,
			})
		}
	}

	return err
}

///////////////////////////////////////////////////////////////////////////////

var cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
var atlaswidth = flag.Int("width", 1024, "width of generated atlas (default 1024)")
var atlasheight = flag.Int("height", 1024, "height of generated atlas (default 1024)")
var atlasfilename = flag.String("out", "atlas.png", "name of generated atlas (default atlas.png)")
var drawpadding = flag.Bool("drawpadding", false, "draw padding around images (debug feature)")
var jsonmeta = flag.String("json", "", "save atlas meta as json")
var as3meta = flag.String("as3", "", "save atlas meta as actionscript")
var as3name = flag.String("as3name", "Atlas", "package and class name of actionscript object (default Atlas)")
var strip = flag.Int("strip", 0, "number of path elements to strip")

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

	if *atlasheight <= 1 || *atlaswidth <= 1 {
		fmt.Println("Invalid width or height")
		os.Exit(1)
	}

	atlas := NewAtlas()

	for _, arg := range flag.Args() {
		err := atlas.AddImages(arg)
		if err != nil {
			panic(err)
		}
	}

	altasSize := image.Pt(*atlaswidth, *atlasheight)

	err := atlas.PackImages2(altasSize)
	if err != nil {
		panic(err)
	}

	err = atlas.SaveAtlasImage(*atlasfilename, altasSize, *drawpadding)
	if err != nil {
		panic(err)
	}

	//err = atlas.SaveAtlasMeta(strings.TrimSuffix(*atlasfilename, ".png")+".json", altasSize)
	if *jsonmeta != "" {
		err = atlas.SaveAtlasMeta(*jsonmeta, *strip, altasSize)
		if err != nil {
			panic(err)
		}
	}

	if *as3meta != "" {
		err = atlas.SaveAtlasMetaAsActionScript(*as3meta, *as3name, *strip, altasSize)
		if err != nil {
			panic(err)
		}
	}
}

///////////////////////////////////////////////////////////////////////////////
