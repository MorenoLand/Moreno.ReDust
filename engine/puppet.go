package engine

import (
	"fmt"
	"image"

	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"

	"redust/assets"
	"redust/audio"
	"redust/render"
)

type PuppetDialogue struct {
	puppet     *render.Puppet
	context    *ebitenaudio.Context
	lines      []assets.PuppetSpeech
	lineIndex  int
	background render.IndexedFrame
	canvas     *render.PuppetCanvas
	cues       []render.PuppetCueRow
	baseClip   image.Rectangle
	voice      *ebitenaudio.Player
	startFrame uint32
	currentCue int
	active     bool
}

func NewPuppetDialogue(puppet *render.Puppet, table []assets.PuppetSpeech, names []string, context *ebitenaudio.Context) (*PuppetDialogue, error) {
	if puppet == nil || len(names) == 0 {
		return nil, fmt.Errorf("puppet dialogue requires a puppet and speech calls")
	}
	byName := make(map[string]assets.PuppetSpeech, len(table))
	for _, speech := range table {
		byName[speech.Name] = speech
	}
	lines := make([]assets.PuppetSpeech, len(names))
	for index, name := range names {
		speech, found := byName[name]
		if !found {
			return nil, fmt.Errorf("puppet speech %q is missing from the line table", name)
		}
		lines[index] = speech
	}
	return &PuppetDialogue{puppet: puppet, context: context, lines: lines}, nil
}

func (d *PuppetDialogue) Start(background render.IndexedFrame, frame uint32) (render.IndexedFrame, error) {
	if d == nil || d.puppet == nil || d.active {
		return render.IndexedFrame{}, fmt.Errorf("puppet dialogue cannot start in its current state")
	}
	d.background, d.lineIndex, d.currentCue = background, 0, -1
	d.setBaseClip()
	canvas, err := d.newCanvas()
	if err != nil {
		return render.IndexedFrame{}, err
	}
	canvas.Fill(0)
	d.canvas = canvas
	if err := d.startLine(frame); err != nil {
		return render.IndexedFrame{}, err
	}
	d.active = true
	return d.frame()
}

func (d *PuppetDialogue) Update(frame uint32) (render.IndexedFrame, bool, error) {
	if d == nil || !d.active {
		return render.IndexedFrame{}, false, nil
	}
	line := d.lines[d.lineIndex]
	sample := int(frame-d.startFrame) / 2
	if sample >= len(d.cues) {
		sample = len(d.cues) - 1
	}
	changed := sample > d.currentCue
	if changed {
		clip := d.baseClip
		if d.currentCue >= 0 {
			var dirty image.Rectangle
			hasDirty := false
			for cue := d.currentCue + 1; cue <= sample; cue++ {
				if !d.cues[cue].HasBounds {
					continue
				}
				if hasDirty {
					dirty = dirty.Union(d.cues[cue].Bounds)
				} else {
					dirty, hasDirty = d.cues[cue].Bounds, true
				}
			}
			if !hasDirty {
				d.currentCue = sample
				changed = false
			} else {
				clip = clip.Intersect(dirty)
			}
		}
		if changed {
			d.canvas.SetClip(clip)
			if err := d.drawCue(d.cues[sample]); err != nil {
				return render.IndexedFrame{}, false, err
			}
			d.canvas.SetClip(d.baseClip)
			d.currentCue = sample
		}
	}
	finished := d.voice != nil && !d.voice.IsPlaying()
	if d.voice == nil {
		finished = sample >= int(line.CueFrameLimit)-1
	}
	if finished {
		if err := d.advance(frame); err != nil {
			return render.IndexedFrame{}, false, err
		}
		changed = true
	}
	if !changed {
		return render.IndexedFrame{}, false, nil
	}
	image, err := d.frame()
	return image, true, err
}

func (d *PuppetDialogue) Skip() (render.IndexedFrame, bool, error) {
	if d == nil || !d.active {
		return render.IndexedFrame{}, false, nil
	}
	if err := d.closeVoice(); err != nil {
		return render.IndexedFrame{}, false, err
	}
	d.active = false
	canvas, err := d.newCanvas()
	if err != nil {
		return render.IndexedFrame{}, false, err
	}
	d.canvas = canvas
	image, err := d.frame()
	return image, true, err
}

func (d *PuppetDialogue) Active() bool {
	return d != nil && d.active
}

func (d *PuppetDialogue) Frame() (render.IndexedFrame, error) {
	return d.frame()
}

func (d *PuppetDialogue) Close() error {
	if d == nil {
		return nil
	}
	d.active = false
	return d.closeVoice()
}

func (d *PuppetDialogue) startLine(frame uint32) error {
	line := d.lines[d.lineIndex]
	cues, err := d.puppet.CueTimeline(line.CueResource)
	if err != nil {
		return fmt.Errorf("load puppet cue resource %d: %w", line.CueResource, err)
	}
	if len(cues) != int(line.CueFrameLimit) || len(cues) == 0 {
		return fmt.Errorf("puppet speech %q has %d cue rows, want %d", line.Name, len(cues), line.CueFrameLimit)
	}
	d.cues, d.startFrame, d.currentCue = cues, frame, -1
	d.setBaseClip()
	if err := d.drawCue(cues[0]); err != nil {
		return err
	}
	d.currentCue = 0
	voice, err := d.puppet.Resource(line.VoiceResource)
	if err != nil {
		return fmt.Errorf("load puppet voice resource %d: %w", line.VoiceResource, err)
	}
	sound, err := audio.DecodeNativeSoundResource(voice)
	if err != nil {
		return fmt.Errorf("decode puppet voice resource %d: %w", line.VoiceResource, err)
	}
	if d.context != nil {
		d.voice, err = audio.NewPlayer(d.context, sound.Samples, sound.Format)
		if err != nil {
			return fmt.Errorf("create puppet voice player: %w", err)
		}
		d.voice.Play()
	}
	return nil
}

func (d *PuppetDialogue) advance(frame uint32) error {
	if err := d.closeVoice(); err != nil {
		return err
	}
	d.lineIndex++
	if d.lineIndex >= len(d.lines) {
		d.active = false
		return nil
	}
	return d.startLine(frame)
}

func (d *PuppetDialogue) newCanvas() (*render.PuppetCanvas, error) {
	canvas, err := render.NewPuppetCanvas(d.background)
	if err == nil {
		canvas.SetClip(d.baseClip)
	}
	return canvas, err
}

func (d *PuppetDialogue) setBaseClip() {
	height := 264
	if d.lineIndex < len(d.lines) && len(d.lines[d.lineIndex].Subtitle) > 0 {
		height = 224
	}
	d.baseClip = image.Rect(0, 0, d.background.Width, min(d.background.Height, height))
	if d.canvas != nil {
		d.canvas.SetClip(d.baseClip)
	}
}

func (d *PuppetDialogue) drawCue(cue render.PuppetCueRow) error {
	return d.puppet.DrawCue(d.canvas, cue)
}

func (d *PuppetDialogue) closeVoice() error {
	if d.voice == nil {
		return nil
	}
	voice := d.voice
	d.voice = nil
	return voice.Close()
}

func (d *PuppetDialogue) frame() (render.IndexedFrame, error) {
	if d == nil || d.canvas == nil {
		return render.IndexedFrame{}, fmt.Errorf("puppet dialogue frame is unavailable")
	}
	frame := d.canvas.Frame()
	var err error
	if d.active && d.lineIndex < len(d.lines) {
		frame, err = render.DrawNativeSubtitle(frame, d.lines[d.lineIndex].SubtitleText())
	}
	return frame, err
}
