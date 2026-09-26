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

func (g *Game) Update() error              { return nil }
func (g *Game) Draw(screen *ebiten.Image)  { screen.DrawImage(g.frame, nil) }
func (g *Game) Layout(_, _ int) (int, int) { return g.width, g.height }

func Run(frame render.IndexedFrame) error {
	game, err := NewGame(frame)
	if err != nil {
		return err
	}
	ebiten.SetWindowTitle("ReDust")
	ebiten.SetWindowSize(game.width, game.height)
	if err := ebiten.RunGame(game); err != nil {
		return fmt.Errorf("run Ebitengine: %w", err)
	}
	return nil
}
