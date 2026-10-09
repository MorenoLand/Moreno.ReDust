package main

import (
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"

	"redust/assets"
	"redust/render"
	"redust/save"
	"redust/scripts"
	"redust/scripts/story"
)

// The options flat is NEW.FLT's "score" flat. Its controls and the scripts
// behind them (resource numbers from the shipped stage file):
//
//	slider           BOOTFILE r1 menuvolume / NEW.FLT r11 openflat
//	subtitles        r42: puppetparam(7) toggle and the "check" prop
//	north/east/west  r27/r29/r30: seldir=me, propxy("keysel", x, y); the flat's
//	                 keydown (r11) stores the next key in keynorth/keyeast/keywest
//	save/open        r24/r25: savegame/opengame("dust 0.3")
//	quit             r31: two questiondialog prompts, savegame, quit()
//	help             r26: sendtostage(spotmovie("help.mov"))
//	credits          r28: runcredits()
//	ok               gotoflat(1) with barndoorclose
//
// optionsMenu holds the part of that state the flat draws: which key box
// `seldir` selects, which button trackbut() (r11) shows pressed, and the
// parsed positions of the "keysel" prop.
type optionsMenu struct {
	seldir string
	bevel  string
	// actions are the parsed handlers of the flat, by lower-case name.
	actions map[string]story.OptionsAction
	loaded  bool
}

// keyGlobalNames maps a key box (the handler name `me`) to the script global
// BOOTFILE r1 declares for it.
var keyGlobalNames = map[string]string{"north": "keynorth", "east": "keyeast", "west": "keywest"}

// keyDefaults are BOOTFILE r1's boot(): keynorth="W", keyeast="D", keywest="A".
var keyDefaults = map[string]string{"keynorth": "W", "keyeast": "D", "keywest": "A"}

// load parses the key and subtitle handlers of the score flat from the stage
// file's own scripts. FUN_00411AD0's flat index 3 is the options flat.
func (o *optionsMenu) load(stage *assets.Stage, scene int) error {
	if o.loaded {
		return nil
	}
	handlers, err := stage.SceneHandlers(scene)
	if err != nil {
		return err
	}
	o.actions = map[string]story.OptionsAction{}
	for _, handler := range handlers {
		lease, err := stage.AcquireResource(handler.ScriptResource)
		if err != nil {
			return err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		program, err := scripts.ParseProgram(data)
		if err != nil {
			return err
		}
		action, found, err := story.ParseOptionsAction(program, string(handler.Name[1:]))
		if err != nil {
			return fmt.Errorf("options handler %s: %w", handler.Name[1:], err)
		}
		if found {
			o.actions[strings.ToLower(string(handler.Name[1:]))] = action
		}
	}
	o.loaded = true
	// NEW.FLT r11 openflat: sendtobutton(me,"north",mousedown(0)).
	if o.seldir == "" {
		o.seldir = "north"
	}
	return nil
}

// enter is the flat's openflat (NEW.FLT r11): the key selector starts on the
// north box (sendtobutton(me,"north",mousedown(0))) and no button is pressed.
func (o *optionsMenu) enter(stage *assets.Stage, scene int) error {
	if err := o.load(stage, scene); err != nil {
		return err
	}
	o.seldir, o.bevel = "north", ""
	return nil
}

func (f *optionsFlow) enter(stage *assets.Stage, scene int) error {
	return f.menu.enter(stage, scene)
}

// optionsEnv is what the options flow needs from the running game.
type optionsEnv struct {
	workDir string
	// frame is the frame on screen; recompose redraws the options flat.
	frame     func() render.IndexedFrame
	recompose func() (render.IndexedFrame, bool, error)
	// flat is gotoflat as the button scripts apply it; returnToFlat is
	// gotoflat(<name>) of a previously captured currentflat().
	flat         func(target int, effect uint16, duration int, source uint32) (render.IndexedFrame, bool, error)
	returnToFlat func(name string, source uint32) (render.IndexedFrame, bool, error)
	// saveGame and loadGame move the whole game state to and from a file
	// named under the saves directory.
	saveGame func(gameName, fileName string) error
	loadGame func(gameName, fileName string) error
	// playMovie runs spotmovie(name) and then after.
	playMovie func(name string, after func() (render.IndexedFrame, bool, error)) error
	// getGlobal and setGlobal read and write script globals.
	getGlobal func(name string) (string, bool)
	setGlobal func(name, value string) error
	params    *[8]int16
	quit      func()
	logf      func(format string, args ...any)
}

// optionsFlow runs the flat's buttons.
type optionsFlow struct {
	env      optionsEnv
	menu     optionsMenu
	modal    modalDialog
	lastName string
}

func (f *optionsFlow) key(name string) string {
	if value, ok := f.env.getGlobal(keyGlobalNames[name]); ok && value != "" {
		return value
	}
	return keyDefaults[keyGlobalNames[name]]
}

// subtitlesOn is puppetparam(7).
func (f *optionsFlow) subtitlesOn() bool { return f.env.params != nil && f.env.params[6] != 0 }

// Sprites are the props the flat shows besides the slider: the subtitles
// check, the key selector and the pressed button's bevel.
func (f *optionsFlow) Sprites(archive *assets.PropArchive) []render.FlatPropSprite {
	var sprites []render.FlatPropSprite
	if action, ok := f.menu.actions["subtitles"]; ok && f.subtitlesOn() {
		sprites = append(sprites, render.FlatPropSprite{Name: "check", PropName: action.Prop, ViewName: "BASE", Anchor: image.Pt(action.X, action.Y), Archive: archive})
	}
	if action, ok := f.menu.actions[f.menu.seldir]; ok {
		sprites = append(sprites, render.FlatPropSprite{Name: "keysel", PropName: action.Prop, ViewName: "BASE", Anchor: image.Pt(action.X, action.Y), Archive: archive})
	}
	if at, ok := story.OptionsButtonBevels[f.menu.bevel]; ok {
		sprites = append(sprites, render.FlatPropSprite{Name: "butbevel", PropName: "butbevel", ViewName: "BASE", Anchor: image.Pt(at[0], at[1]), Archive: archive})
	}
	return sprites
}

// keyTextPositions are NEW.FLT r11 update(): drawstring(key, makepoint(x, y),
// 0, 12) for keynorth, keyeast and keywest.
var keyTextPositions = []struct {
	name string
	at   image.Point
}{{"north", image.Pt(390, 235)}, {"east", image.Pt(435, 277)}, {"west", image.Pt(347, 277)}}

// DrawKeys draws the three key names the flat's update loop draws.
func (f *optionsFlow) DrawKeys(frame render.IndexedFrame) (render.IndexedFrame, error) {
	if len(frame.Palette) <= 12 {
		return frame, nil
	}
	var err error
	for _, entry := range keyTextPositions {
		if frame, err = render.DrawNativeTextAtColor(frame, f.key(entry.name), entry.at, frame.Palette[0]); err != nil {
			return render.IndexedFrame{}, err
		}
	}
	return frame, nil
}

// keyCharacter is the argument the engine's key handler gives the flat's
// keydown: the key's character. NEW.FLT r11 checkey() accepts a..z, 0..9 and a
// space, returning them lower-case.
func keyCharacter(key ebiten.Key) string {
	switch {
	case key >= ebiten.KeyA && key <= ebiten.KeyZ:
		return string(rune('a' + int(key-ebiten.KeyA)))
	case key >= ebiten.Key0 && key <= ebiten.Key9:
		return string(rune('0' + int(key-ebiten.Key0)))
	case key == ebiten.KeySpace:
		return " "
	}
	return ""
}

// KeyDown is r11's keydown(arg): store the typed key in the selected box's
// global and redraw the flat.
func (f *optionsFlow) KeyDown(key ebiten.Key) (render.IndexedFrame, bool, error) {
	text := keyCharacter(key)
	global, ok := keyGlobalNames[f.menu.seldir]
	if text == "" || !ok {
		return render.IndexedFrame{}, false, nil
	}
	if err := f.env.setGlobal(global, text); err != nil {
		return render.IndexedFrame{}, false, err
	}
	if f.env.logf != nil {
		f.env.logf("options key %s=%q", global, text)
	}
	return f.env.recompose()
}

// MovementKey translates a physical key to the arrow key the world handlers
// take. BOOTFILE r1 keydown() maps a key equal to keynorth/keywest/keyeast to
// "uparrow"/"leftarrow"/"rightarrow" before it reaches the scene script, so
// a reassigned letter moves the player and the old default letter stops
// doing so. The arrow keys always work.
func (f *optionsFlow) MovementKey(key ebiten.Key) ebiten.Key {
	if f.env.getGlobal == nil {
		return key
	}
	switch key {
	case ebiten.KeyArrowUp, ebiten.KeyArrowDown, ebiten.KeyArrowLeft, ebiten.KeyArrowRight:
		return key
	}
	text := keyCharacter(key)
	if text == "" {
		return key
	}
	for _, entry := range []struct {
		name   string
		target ebiten.Key
	}{{"north", ebiten.KeyArrowUp}, {"west", ebiten.KeyArrowLeft}, {"east", ebiten.KeyArrowRight}} {
		if strings.EqualFold(f.key(entry.name), text) {
			return entry.target
		}
	}
	switch key {
	case ebiten.KeyW, ebiten.KeyA, ebiten.KeyD:
		// The default letters stop moving once their box holds another key.
		return ebiten.KeyF24
	}
	return key
}

// Click handles the handlers that act on the flat itself. handler is the
// button's name.
func (f *optionsFlow) Click(handler string) (render.IndexedFrame, bool, error) {
	action, ok := f.menu.actions[strings.ToLower(handler)]
	if !ok {
		return render.IndexedFrame{}, false, nil
	}
	switch action.Kind {
	case story.OptionsActionSubtitles:
		if f.env.params == nil {
			return render.IndexedFrame{}, false, nil
		}
		if f.env.params[action.Param-1] == 0 {
			f.env.params[action.Param-1] = 1
		} else {
			f.env.params[action.Param-1] = 0
		}
		if f.env.logf != nil {
			f.env.logf("options subtitles=%t", f.subtitlesOn())
		}
	case story.OptionsActionKeySelect:
		f.menu.seldir = action.Direction
	default:
		return render.IndexedFrame{}, false, nil
	}
	return f.env.recompose()
}

// SetBevel shows or hides the pressed-button bevel (r11 trackbut()).
func (f *optionsFlow) SetBevel(handler string) (render.IndexedFrame, bool, error) {
	handler = strings.ToLower(handler)
	if _, ok := story.OptionsButtonBevels[handler]; !ok {
		handler = ""
	}
	if f.menu.bevel == handler {
		return render.IndexedFrame{}, false, nil
	}
	f.menu.bevel = handler
	return f.env.recompose()
}

func errorNotice(err error) string {
	switch {
	case errors.Is(err, save.ErrDifferentVersion):
		return save.ErrDifferentVersion.Error()
	case errors.Is(err, save.ErrNotSavedGame):
		return save.ErrNotSavedGame.Error()
	}
	return err.Error()
}

// Save is r24 after the button was released over itself: gotoflat(1), the
// save dialog and the file write, then gotoflat(<captured flat>).
func (f *optionsFlow) Save(gameName, returnFlat string, source uint32) (render.IndexedFrame, bool, error) {
	f.menu.bevel = ""
	base, _, err := f.env.flat(0, 0, 0, source)
	if err != nil {
		return render.IndexedFrame{}, false, err
	}
	back := func() (render.IndexedFrame, bool, error) {
		if returnFlat == "" {
			return f.env.frame(), true, nil
		}
		return f.env.returnToFlat(returnFlat, source)
	}
	return f.modal.Choose(base, f.env.workDir, true, f.lastName, func(name string, ok bool) (render.IndexedFrame, bool, error) {
		if !ok {
			return back()
		}
		if err := f.env.saveGame(gameName, name); err != nil {
			return f.modal.Notice(base, "The game could not be saved.\n"+errorNotice(err), back)
		}
		f.lastName = name
		return back()
	})
}

// Open is r25: the open dialog, the load, and nothing else; the flat's update
// loop restarts only if the load left the player on the score flat.
func (f *optionsFlow) Open(gameName string) (render.IndexedFrame, bool, error) {
	f.menu.bevel = ""
	base := f.env.frame()
	return f.modal.Choose(base, f.env.workDir, false, f.lastName, func(name string, ok bool) (render.IndexedFrame, bool, error) {
		if !ok {
			return base, true, nil
		}
		if err := f.env.loadGame(gameName, name); err != nil {
			return f.modal.Notice(base, errorNotice(err), func() (render.IndexedFrame, bool, error) {
				return base, true, nil
			})
		}
		f.lastName = name
		return f.env.frame(), true, nil
	})
}

// Quit is r31 (or r35 on the death flat): confirm, offer to save, quit.
func (f *optionsFlow) Quit(prompts []string, gameName string) (render.IndexedFrame, bool, error) {
	f.menu.bevel = ""
	base := f.env.frame()
	finish := func() (render.IndexedFrame, bool, error) {
		f.env.quit()
		return base, true, nil
	}
	saveFirst := func(prompt string) (render.IndexedFrame, bool, error) {
		return f.modal.Question(base, prompt, func(yes bool) (render.IndexedFrame, bool, error) {
			if !yes || gameName == "" {
				return finish()
			}
			// savegame() cancelled or failed still falls through to quit().
			return f.modal.Choose(base, f.env.workDir, true, f.lastName, func(name string, ok bool) (render.IndexedFrame, bool, error) {
				if ok {
					if err := f.env.saveGame(gameName, name); err != nil {
						return f.modal.Notice(base, "The game could not be saved.\n"+errorNotice(err), finish)
					}
					f.lastName = name
				}
				return finish()
			})
		})
	}
	switch len(prompts) {
	case 0:
		return finish()
	case 1:
		return saveFirst(prompts[0])
	}
	return f.modal.Question(base, prompts[0], func(yes bool) (render.IndexedFrame, bool, error) {
		if !yes {
			return base, true, nil
		}
		return saveFirst(prompts[1])
	})
}

// Help is r26: spotmovie("help.mov") through the stage, then the flat again.
func (f *optionsFlow) Help(movie string) (render.IndexedFrame, bool, error) {
	f.menu.bevel = ""
	if err := f.env.playMovie(movie, f.env.recompose); err != nil {
		return render.IndexedFrame{}, false, err
	}
	return f.env.frame(), true, nil
}
