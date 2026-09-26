package engine

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"

	"redust/render"
)

type Game struct {
	frame  *ebiten.Image
	width  int
	height int
}

func NewGame(frame render.IndexedFrame) (*Game, error) {
	image, err := frame.EbitenImage()
	if err != nil {
		return nil, err
	}
	return &Game{frame: image, width: frame.Width, height: frame.Height}, nil
}

func (g *Game) Update() error { return nil }
func (g *Game) Draw(screen *ebiten.Image) {
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(screen.Bounds().Dx())/float64(g.width), float64(screen.Bounds().Dy())/float64(g.height))
	screen.DrawImage(g.frame, op)
}
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	if outsideWidth <= 0 || outsideHeight <= 0 {
		return g.width, g.height
	}
	return outsideWidth, outsideHeight
}

func Run(frame render.IndexedFrame) error {
	game, err := NewGame(frame)
	if err != nil {
		return err
	}
	ebiten.SetWindowTitle("ReDust")
	ebiten.SetWindowSize(game.width, game.height)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(game); err != nil {
		return fmt.Errorf("run Ebitengine: %w", err)
	}
	return nil
}
