package engine

import (
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	onUpdate     func() (render.IndexedFrame, bool, error)
	keyDown      func(ebiten.Key)
	mouseDown    func(MouseEvent) (render.IndexedFrame, bool, error)
	mouseState   func(MouseState) (render.IndexedFrame, bool, error)
}

type MouseEvent struct {
	Point  uint32
	Button ebiten.MouseButton
}

type MouseState struct {
	Point        uint32
	LeftDown     bool
	LeftReleased bool
}

func NewGame(frame render.IndexedFrame) (*Game, error) {
	image, err := frame.EbitenImage()
	if err != nil {
		return nil, err
	}
	return &Game{frame: image, width: frame.Width, height: frame.Height}, nil
}

func (g *Game) Update() error {
	if g.keyDown != nil {
		for _, key := range []ebiten.Key{ebiten.KeyEscape, ebiten.KeySpace, ebiten.KeyQ, ebiten.KeyPeriod, ebiten.KeyArrowUp, ebiten.KeyArrowDown, ebiten.KeyArrowLeft, ebiten.KeyArrowRight, ebiten.KeyW, ebiten.KeyA, ebiten.KeyS, ebiten.KeyD, ebiten.Key0, ebiten.Key1, ebiten.Key2, ebiten.Key3, ebiten.Key4, ebiten.Key5, ebiten.Key6, ebiten.Key7, ebiten.Key8, ebiten.Key9} {
			if inpututil.IsKeyJustPressed(key) {
				g.keyDown(key)
				break
			}
		}
	}
	if g.onUpdate != nil {
		frame, changed, err := g.onUpdate()
		if err != nil {
			return err
		}
		if changed {
			image, err := frame.EbitenImage()
			if err != nil {
				return err
			}
			g.frame, g.width, g.height = image, frame.Width, frame.Height
		}
	}
	if g.mouseDown != nil {
		for _, button := range []ebiten.MouseButton{ebiten.MouseButtonLeft, ebiten.MouseButtonRight} {
			if !inpututil.IsMouseButtonJustPressed(button) {
				continue
			}
			frame, changed, err := g.mouseDown(MouseEvent{Point: g.pointerPoint(), Button: button})
			if err != nil {
				return err
			}
			if changed {
				image, err := frame.EbitenImage()
				if err != nil {
					return err
				}
				g.frame, g.width, g.height = image, frame.Width, frame.Height
			}
		}
	}
	if g.mouseState != nil {
		frame, changed, err := g.mouseState(MouseState{Point: g.pointerPoint(), LeftDown: ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft), LeftReleased: inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft)})
		if err != nil {
			return err
		}
		if changed {
			image, err := frame.EbitenImage()
			if err != nil {
				return err
			}
			g.frame, g.width, g.height = image, frame.Width, frame.Height
		}
	}
	return nil
}

func (g *Game) pointerPoint() uint32 {
	x, y := ebiten.CursorPosition()
	if g.screenWidth > 0 && g.screenHeight > 0 {
		x, y = x*g.width/g.screenWidth, y*g.height/g.screenHeight
	}
	return uint32(uint16(x))<<16 | uint32(uint16(y))
}
func (g *Game) Draw(screen *ebiten.Image) {
	op := &ebiten.DrawImageOptions{}
	op.Filter = ebiten.FilterNearest
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

func Run(frame render.IndexedFrame, onUpdate func() (render.IndexedFrame, bool, error), keyDown func(ebiten.Key), mouseDown func(MouseEvent) (render.IndexedFrame, bool, error), mouseState func(MouseState) (render.IndexedFrame, bool, error)) error {
	game, err := NewGame(frame)
	if err != nil {
		return err
	}
	game.onUpdate, game.keyDown, game.mouseDown, game.mouseState = onUpdate, keyDown, mouseDown, mouseState
	icon, err := render.AppIcon()
	if err != nil {
		return fmt.Errorf("load ReDust window icon: %w", err)
	}
	ebiten.SetWindowIcon([]image.Image{icon})
	ebiten.SetWindowTitle("ReDust")
	ebiten.SetWindowSize(game.width, game.height)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	ebiten.SetScreenFilterEnabled(false)
	if err := ebiten.RunGame(game); err != nil {
		return fmt.Errorf("run Ebitengine: %w", err)
	}
	return nil
}

type silentAction struct {
	Type         string `json:"type"`
	Key          string `json:"key,omitempty"`
	Button       string `json:"button,omitempty"`
	X            *int   `json:"x,omitempty"`
	Y            *int   `json:"y,omitempty"`
	Milliseconds *int   `json:"milliseconds,omitempty"`
	Path         string `json:"path,omitempty"`
}

var silentKeys = map[string]ebiten.Key{"Escape": ebiten.KeyEscape, "Space": ebiten.KeySpace, "Q": ebiten.KeyQ, "Period": ebiten.KeyPeriod, "ArrowUp": ebiten.KeyArrowUp, "ArrowDown": ebiten.KeyArrowDown, "ArrowLeft": ebiten.KeyArrowLeft, "ArrowRight": ebiten.KeyArrowRight, "W": ebiten.KeyW, "A": ebiten.KeyA, "S": ebiten.KeyS, "D": ebiten.KeyD, "0": ebiten.Key0, "1": ebiten.Key1, "2": ebiten.Key2, "3": ebiten.Key3, "4": ebiten.Key4, "5": ebiten.Key5, "6": ebiten.Key6, "7": ebiten.Key7, "8": ebiten.Key8, "9": ebiten.Key9}

func SilentRunner(scriptPath string) func(render.IndexedFrame, func() (render.IndexedFrame, bool, error), func(ebiten.Key), func(MouseEvent) (render.IndexedFrame, bool, error), func(MouseState) (render.IndexedFrame, bool, error)) error {
	return func(frame render.IndexedFrame, onUpdate func() (render.IndexedFrame, bool, error), keyDown func(ebiten.Key), mouseDown func(MouseEvent) (render.IndexedFrame, bool, error), mouseState func(MouseState) (render.IndexedFrame, bool, error)) error {
		return RunSilent(frame, onUpdate, keyDown, mouseDown, mouseState, scriptPath)
	}
}

func RunSilent(frame render.IndexedFrame, onUpdate func() (render.IndexedFrame, bool, error), keyDown func(ebiten.Key), mouseDown func(MouseEvent) (render.IndexedFrame, bool, error), mouseState func(MouseState) (render.IndexedFrame, bool, error), scriptPath string) error {
	file, err := os.Open(scriptPath)
	if err != nil {
		return fmt.Errorf("open silent script: %w", err)
	}
	defer file.Close()
	var script struct {
		Actions []silentAction `json:"actions"`
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&script); err != nil {
		return fmt.Errorf("decode silent script: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("silent script contains trailing JSON")
	}
	if len(script.Actions) == 0 {
		return fmt.Errorf("silent script has no actions")
	}
	outputRoot, err := filepath.Abs("bin")
	if err != nil {
		return err
	}
	for index, action := range script.Actions {
		if err := validateSilentAction(action, outputRoot); err != nil {
			return fmt.Errorf("silent action %d: %w", index+1, err)
		}
	}
	point, held := uint32(0), [2]bool{}
	apply := func(next render.IndexedFrame, changed bool, err error) error {
		if err == nil && changed {
			frame = next
		}
		return err
	}
	step := func(key *ebiten.Key, button *ebiten.MouseButton, released bool) error {
		if key != nil && keyDown != nil {
			keyDown(*key)
		}
		if onUpdate != nil {
			if err := apply(onUpdate()); err != nil {
				return err
			}
		}
		if button != nil && mouseDown != nil {
			if err := apply(mouseDown(MouseEvent{Point: point, Button: *button})); err != nil {
				return err
			}
		}
		if mouseState != nil {
			return apply(mouseState(MouseState{Point: point, LeftDown: held[0], LeftReleased: released}))
		}
		return nil
	}
	for index, action := range script.Actions {
		if action.X != nil {
			point = uint32(uint16(*action.X))<<16 | uint32(uint16(*action.Y))
		}
		button := ebiten.MouseButtonLeft
		if action.Button == "right" {
			button = ebiten.MouseButtonRight
		}
		switch action.Type {
		case "key":
			key := silentKeys[action.Key]
			err = step(&key, nil, false)
		case "mouse_down", "click":
			if held[button] {
				return fmt.Errorf("silent action %d: mouse button is already down", index+1)
			}
			held[button] = true
			err = step(nil, &button, false)
			if err == nil && action.Type == "click" {
				held[button] = false
				err = step(nil, nil, button == ebiten.MouseButtonLeft)
			}
		case "mouse_up":
			if !held[button] {
				return fmt.Errorf("silent action %d: mouse button is not down", index+1)
			}
			held[button] = false
			err = step(nil, nil, button == ebiten.MouseButtonLeft)
		case "mouse_move":
			err = step(nil, nil, false)
		case "wait":
			deadline := time.Now().Add(time.Duration(*action.Milliseconds) * time.Millisecond)
			for err == nil {
				remaining := time.Until(deadline)
				if remaining > 0 {
					time.Sleep(min(remaining, time.Second/60))
				}
				err = step(nil, nil, false)
				if remaining <= time.Second/60 {
					break
				}
			}
		case "snapshot":
			path, _ := filepath.Abs(action.Path)
			if err = os.MkdirAll(filepath.Dir(path), 0755); err == nil {
				err = render.WritePNG(path, frame)
			}
		}
		if err != nil {
			return fmt.Errorf("silent action %d (%s): %w", index+1, action.Type, err)
		}
	}
	return nil
}

func validateSilentAction(action silentAction, outputRoot string) error {
	switch action.Type {
	case "key":
		if _, exists := silentKeys[action.Key]; !exists {
			return fmt.Errorf("unknown key %q", action.Key)
		}
		if action.X != nil || action.Y != nil || action.Milliseconds != nil || action.Button != "" || action.Path != "" {
			return fmt.Errorf("key action has unrelated fields")
		}
	case "mouse_down", "mouse_up", "mouse_move", "click":
		if action.X == nil || action.Y == nil || *action.X < 0 || *action.X > 65535 || *action.Y < 0 || *action.Y > 65535 {
			return fmt.Errorf("mouse action requires x and y in 0..65535")
		}
		if action.Button != "" && action.Button != "left" && action.Button != "right" || action.Type == "mouse_move" && action.Button != "" {
			return fmt.Errorf("unknown mouse button %q", action.Button)
		}
		if action.Key != "" || action.Milliseconds != nil || action.Path != "" {
			return fmt.Errorf("mouse action has unrelated fields")
		}
	case "wait":
		if action.Milliseconds == nil || *action.Milliseconds < 0 || *action.Milliseconds > 60000 {
			return fmt.Errorf("wait requires milliseconds in 0..60000")
		}
		if action.X != nil || action.Y != nil || action.Key != "" || action.Button != "" || action.Path != "" {
			return fmt.Errorf("wait action has unrelated fields")
		}
	case "snapshot":
		path, err := filepath.Abs(action.Path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(outputRoot, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || strings.ToLower(filepath.Ext(path)) != ".png" {
			return fmt.Errorf("snapshot path must be a PNG inside bin")
		}
		if action.X != nil || action.Y != nil || action.Key != "" || action.Button != "" || action.Milliseconds != nil {
			return fmt.Errorf("snapshot action has unrelated fields")
		}
	default:
		return fmt.Errorf("unknown action %q", action.Type)
	}
	return nil
}
