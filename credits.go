package main

import (
	"fmt"
	"image"
	"strconv"
	"time"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"

	"redust/assets"
	"redust/audio"
	"redust/render"
)

// The credits screen is what NEW.FLT r28 runcredits() starts:
//
//	framerate(1)  screentoblack("current",10)  blackscreen()
//	closestagefile()  openstagefile("credits.flt")  openshopfile("credits.prp")
//	forceupdate()  blacktoscreen("stage",10)  sendtoshop("credits.prp",scrollnames())
//
// CREDITS.FLT r1 openstage() starts the "credits" theme from CREDITS.SND and
// picks theflat=random(21); CREDITS.PRP r1 openshop() shows the "black" prop at
// (256,192) and selects the "names" view "1". scrollnames() puts "names" at a
// start y per view and namesup() moves it up 3 each frame (makeloop "prop"
// "names" every 1) until propdone() says the block has left, then
// nextflat() fades to black, advances theflat (wrapping 21 to 1), fades back
// in and scrolls the next view (1..8, wrapping). A click anywhere runs
// closecredits(): framerate(3), fade out, reopen new.flt, gotoflat(4), fade in.
//
// At framerate(1) the frame unit is 50 ms (FUN_00424890 clock units are
// ticks*rate/50), so the fade lengths below are the script's 10 and 20 frames
// at that rate and the scroll step is 3 pixels per 50 ms.
const (
	creditsFlatCount = 21
	creditsViewCount = 8
	creditsUnit      = 50 * time.Millisecond
)

// creditsStart is CREDITS.PRP r1 scrollnames(): the y the "names" prop starts
// at for each view, and creditsDone is propdone(): the y it is finished below.
var creditsStart = [creditsViewCount]int{888, 985, 1267, 1112, 927, 906, 980, 789}
var creditsDone = [creditsViewCount]int{-624, -721, -1003, -848, -663, -642, -716, -525}

type creditsPhase int

const (
	creditsFadeOut creditsPhase = iota
	creditsFadeIn
	creditsScroll
	creditsNextOut
	creditsNextIn
	creditsCloseOut
	creditsCloseIn
)

type creditsEnv struct {
	workspace    assets.Workspace
	audioContext *ebitenaudio.Context
	// random is random(n): 1..n.
	random func(n uint32) uint32
	// pauseTheme and resumeTheme stop and restart the theme that was playing
	// (CREDITS.FLT openstage/closestage keep it in trackname).
	pauseTheme  func()
	resumeTheme func()
	// restore is the options flat again, after closecredits().
	restore func() (render.IndexedFrame, bool, error)
	logf    func(format string, args ...any)
}

type creditsScreen struct {
	env        creditsEnv
	active     bool
	phase      creditsPhase
	phaseStart time.Time
	lastScroll time.Time
	before     render.IndexedFrame
	stage      *assets.Stage
	props      *assets.PropArchive
	bank       *audio.SoundBank
	theme      *audio.Player
	flat       int
	view       int
	y          int
	flatFrame  render.IndexedFrame
	after      render.IndexedFrame
}

func (c *creditsScreen) Active() bool { return c != nil && c.active }

// Start is runcredits() with the options flat on screen as before.
func (c *creditsScreen) Start(before render.IndexedFrame) (render.IndexedFrame, bool, error) {
	c.before, c.phase, c.phaseStart, c.active = before, creditsFadeOut, time.Now(), true
	return before, true, nil
}

func (c *creditsScreen) logf(format string, args ...any) {
	if c.env.logf != nil {
		c.env.logf(format, args...)
	}
}

// open is the stage part of runcredits(): the files, the theme, the first flat.
func (c *creditsScreen) open() error {
	var err error
	if c.stage, err = c.env.workspace.OpenStage("DATA/CREDITS.FLT"); err != nil {
		return err
	}
	if c.props, err = c.env.workspace.OpenPropArchive("DATA/CREDITS.PRP"); err != nil {
		return err
	}
	if c.env.pauseTheme != nil {
		c.env.pauseTheme()
	}
	if c.env.audioContext != nil {
		bank, bankErr := audio.OpenSoundBank(c.env.workspace, "DATA/CREDITS.SND")
		if bankErr == nil {
			var theme audio.NativeTheme
			for _, name := range []string{"credits", "credits.snd"} {
				if theme, bankErr = bank.LoadTheme(name); bankErr == nil {
					break
				}
			}
			if bankErr == nil {
				c.theme, bankErr = theme.Play(c.env.audioContext)
			}
			c.bank = bank
		}
		if bankErr != nil {
			c.logf("credits theme: %v", bankErr)
		}
	}
	c.flat = int(c.env.random(creditsFlatCount))
	c.view = 1
	return c.loadFlat()
}

// loadFlat is gotoflat(theflat) followed by scrollnames().
func (c *creditsScreen) loadFlat() error {
	index := c.flat - 1
	if index < 0 || index >= len(c.stage.Scenes) {
		return fmt.Errorf("credits flat %d is outside the %d flats of CREDITS.FLT", c.flat, len(c.stage.Scenes))
	}
	lease, err := c.stage.AcquireSceneFrameResource(index)
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
	pixels, decodeErr := render.DecodeMoviePixels(data, nil)
	if len(pixels.Pixels) == 0 {
		return fmt.Errorf("decode credits flat %d: %w", c.flat, decodeErr)
	}
	if c.flatFrame, err = render.StageFrame(c.stage, pixels.Pixels); err != nil {
		return err
	}
	c.y = creditsStart[c.view-1]
	c.lastScroll = time.Now()
	c.logf("credits flat=%d names=%d", c.flat, c.view)
	return nil
}

// frame composes the flat with the "black" and "names" props.
func (c *creditsScreen) frame() (render.IndexedFrame, error) {
	sprites := []render.FlatPropSprite{
		{Name: "black", PropName: "black", ViewName: "UNTITLED", Anchor: image.Pt(256, 192), Archive: c.props},
		{Name: "names", PropName: "names", ViewName: strconv.Itoa(c.view), Anchor: image.Pt(256, c.y), Archive: c.props},
	}
	frame, _, err := render.CompositeFlatProps(c.flatFrame, sprites)
	return frame, err
}

func (c *creditsScreen) elapsed(length time.Duration) (int, bool) {
	since := time.Since(c.phaseStart)
	if since >= length {
		return 256, true
	}
	return int(since * 256 / length), false
}

func (c *creditsScreen) close() {
	if c.theme != nil {
		_ = c.theme.Close()
		c.theme = nil
	}
	if c.bank != nil {
		_ = c.bank.Close()
		c.bank = nil
	}
	if c.props != nil {
		_ = c.props.Close()
		c.props = nil
	}
	if c.stage != nil {
		_ = c.stage.Close()
		c.stage = nil
	}
}

// Click is CREDITS.FLT r1 mousedown(): closecredits().
func (c *creditsScreen) Click() (render.IndexedFrame, bool, error) {
	if c.phase == creditsCloseOut || c.phase == creditsCloseIn {
		return render.IndexedFrame{}, false, nil
	}
	if c.phase == creditsFadeOut {
		// The stage never opened: just go back.
		c.phase, c.phaseStart = creditsCloseIn, time.Now()
		c.close()
		return render.IndexedFrame{}, false, nil
	}
	c.phase, c.phaseStart = creditsCloseOut, time.Now()
	return render.IndexedFrame{}, false, nil
}

// Update advances the credits and returns the frame to show.
func (c *creditsScreen) Update() (render.IndexedFrame, bool, error) {
	if !c.Active() {
		return render.IndexedFrame{}, false, nil
	}
	switch c.phase {
	case creditsFadeOut:
		level, done := c.elapsed(10 * creditsUnit)
		if !done {
			return render.DimFrame(c.before, 256-level), true, nil
		}
		if err := c.open(); err != nil {
			c.close()
			c.active = false
			if c.env.resumeTheme != nil {
				c.env.resumeTheme()
			}
			frame, _, restoreErr := c.env.restore()
			if restoreErr != nil {
				return render.IndexedFrame{}, false, restoreErr
			}
			return frame, true, fmt.Errorf("open credits: %w", err)
		}
		c.phase, c.phaseStart = creditsFadeIn, time.Now()
		return render.DimFrame(c.before, 0), true, nil
	case creditsFadeIn, creditsNextIn:
		length := 10 * creditsUnit
		if c.phase == creditsNextIn {
			length = 20 * creditsUnit
		}
		level, done := c.elapsed(length)
		frame, err := c.frame()
		if err != nil {
			return render.IndexedFrame{}, false, err
		}
		if done {
			c.phase, c.lastScroll = creditsScroll, time.Now()
			return frame, true, nil
		}
		return render.DimFrame(frame, level), true, nil
	case creditsScroll:
		steps := int(time.Since(c.lastScroll) / creditsUnit)
		if steps == 0 {
			return render.IndexedFrame{}, false, nil
		}
		c.lastScroll = c.lastScroll.Add(time.Duration(steps) * creditsUnit)
		c.y -= 3 * steps
		if c.y < creditsDone[c.view-1] {
			c.phase, c.phaseStart = creditsNextOut, time.Now()
			return render.IndexedFrame{}, false, nil
		}
		frame, err := c.frame()
		return frame, err == nil, err
	case creditsNextOut:
		level, done := c.elapsed(20 * creditsUnit)
		if !done {
			frame, err := c.frame()
			return render.DimFrame(frame, 256-level), err == nil, err
		}
		// nextflat(): the next view, the next flat, wrapping.
		c.view = c.view%creditsViewCount + 1
		c.flat = c.flat%creditsFlatCount + 1
		if err := c.loadFlat(); err != nil {
			return render.IndexedFrame{}, false, err
		}
		c.phase, c.phaseStart = creditsNextIn, time.Now()
		return render.DimFrame(c.flatFrame, 0), true, nil
	case creditsCloseOut:
		level, done := c.elapsed(3 * creditsUnit)
		if !done {
			frame, err := c.frame()
			return render.DimFrame(frame, 256-level), err == nil, err
		}
		c.close()
		if c.env.resumeTheme != nil {
			c.env.resumeTheme()
		}
		frame, _, err := c.env.restore()
		if err != nil {
			return render.IndexedFrame{}, false, err
		}
		c.after = frame
		c.phase, c.phaseStart = creditsCloseIn, time.Now()
		return render.DimFrame(frame, 0), true, nil
	case creditsCloseIn:
		level, done := c.elapsed(3 * creditsUnit)
		if c.after.Width == 0 {
			frame, _, err := c.env.restore()
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			c.after = frame
		}
		if done {
			c.active = false
			c.logf("credits closed")
			return c.after, true, nil
		}
		return render.DimFrame(c.after, level), true, nil
	}
	return render.IndexedFrame{}, false, nil
}
