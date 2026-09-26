package engine

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"redust/render"
)

type Game struct {
	frame        *ebiten.Image
	width        int
	height       int
	screenWidth  int
	screenHeight int
	mouseDown    func(uint32) error
}

func NewGame(frame render.IndexedFrame) (*Game, error) {
	image, err := frame.EbitenImage()
	if err != nil {
		return nil, err
	}
	return &Game{frame: image, width: frame.Width, height: frame.Height}, nil
}

func (g *Game) Update() error {
	if g.mouseDown == nil {
		return nil
	}
	for _, button := range []ebiten.MouseButton{ebiten.MouseButtonLeft, ebiten.MouseButtonRight} {
		if !inpututil.IsMouseButtonJustPressed(button) {
			continue
		}
		x, y := ebiten.CursorPosition()
		if g.screenWidth > 0 && g.screenHeight > 0 {
			x, y = x*g.width/g.screenWidth, y*g.height/g.screenHeight
		}
		if err := g.mouseDown(uint32(uint16(x))<<16 | uint32(uint16(y))); err != nil {
			return err
		}
	}
	return nil
}
func (g *Game) Draw(screen *ebiten.Image) {
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(screen.Bounds().Dx())/float64(g.width), float64(screen.Bounds().Dy())/float64(g.height))
	screen.DrawImage(g.frame, op)
}
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	if outsideWidth <= 0 || outsideHeight <= 0 {
		g.screenWidth, g.screenHeight = g.width, g.height
		return g.width, g.height
	}
	g.screenWidth, g.screenHeight = outsideWidth, outsideHeight
	return outsideWidth, outsideHeight
}

func Run(frame render.IndexedFrame, mouseDown func(uint32) error) error {
	game, err := NewGame(frame)
	if err != nil {
		return err
	}
	game.mouseDown = mouseDown
	ebiten.SetWindowTitle("ReDust")
	ebiten.SetWindowSize(game.width, game.height)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(game); err != nil {
		return fmt.Errorf("run Ebitengine: %w", err)
	}
	return nil
}
