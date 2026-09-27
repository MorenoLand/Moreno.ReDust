package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	ebitenaudio "github.com/hajimehoshi/ebiten/v2/audio"

	"redust/assets"
	"redust/audio"
	"redust/engine"
	"redust/render"
	"redust/scripts"
)

func run() error {
	work := flag.String("work", "bin", "work directory; assets are read from its assets child")
	debug := flag.Bool("debug", false, "enable diagnostics")
	silent := flag.Bool("silent", false, "render the initial level to a PNG and exit without opening a window")
	flag.Parse()
	workspace, err := assets.NewWorkspace(*work)
	if err != nil {
		return fmt.Errorf("initialize work directory: %w", err)
	}
	if *debug {
		log.Printf("work=%s assets=%s", workspace.WorkDir, workspace.AssetRoot)
	}
	boot, err := workspace.OpenResourceCache("BootFile")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("user game files are required under %s; BootFile was not found: %w", workspace.AssetRoot, err)
		}
		return fmt.Errorf("open BootFile: %w", err)
	}
	defer boot.Close()
	if *debug {
		header, err := boot.Header()
		if err != nil {
			return fmt.Errorf("read BootFile header: %w", err)
		}
		log.Printf("BootFile APPL header: entries=%d pages=%d size=%d", header.CountB, header.CountA>>7, header.FileSize)
	}
	gangCast, err := workspace.OpenCast("DATA/GANG.CST")
	if err != nil {
		return fmt.Errorf("open actor cast: %w", err)
	}
	extraCast, err := workspace.OpenCast("DATA/EXTRA.CST")
	if err != nil {
		return fmt.Errorf("open extra actor cast: %w", err)
	}
	if *debug {
		log.Printf("cast-file=DATA/GANG.CST actors=%d", len(gangCast.Actors))
		log.Printf("cast-file=DATA/EXTRA.CST actors=%d", len(extraCast.Actors))
	}
	var soundBank *audio.SoundBank
	var themeBank *audio.SoundBank
	var themePlayer *ebitenaudio.Player
	var audioContext *ebitenaudio.Context
	var nativeLoops scripts.LoopScheduler
	var nativeRandom scripts.NativeRandom
	var currentThemeName string
	if !*silent {
		soundBank, err = audio.OpenSoundBank(workspace, "DATA/UNILIB.SND")
		if err != nil {
			return fmt.Errorf("open startup sound bank: %w", err)
		}
		defer func() { _ = soundBank.Close() }()
		audioContext = ebitenaudio.NewContext(44100)
	}
	defer func() {
		if themePlayer != nil {
			_ = themePlayer.Close()
		}
		if themeBank != nil {
			_ = themeBank.Close()
		}
	}()
	if *debug && soundBank != nil {
		log.Printf("sound-bank=DATA/UNILIB.SND samples=%d", len(soundBank.Names()))
	}
	stage, err := workspace.OpenStage("DATA/NEW.FLT")
	if err != nil {
		return fmt.Errorf("open initial stage: %w", err)
	}
	lease, err := stage.AcquireSceneFrameResource(0)
	if err != nil {
		stage.Close()
		return fmt.Errorf("load initial stage scene 0: %w", err)
	}
	data, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		stage.Close()
		return fmt.Errorf("read initial stage frame resource: %w", err)
	}
	pixels, err := render.DecodeMoviePixels(data, nil)
	if err != nil {
		stage.Close()
		return fmt.Errorf("decode initial stage scene 0: %w", err)
	}
	frame, err := render.StageFrame(stage, pixels.Pixels)
	if err != nil {
		stage.Close()
		return fmt.Errorf("render initial stage scene 0: %w", err)
	}
	if *debug {
		log.Printf("scene=%s/%s size=%dx%d resource=%d", stage.Name[1:], stage.Scenes[0].Name[1:], frame.Width, frame.Height, stage.Scenes[0].Fields[1])
	}
	nightSet, err := workspace.OpenSet("DATA/NITE.SET")
	if err != nil {
		stage.Close()
		return fmt.Errorf("open startup game set: %w", err)
	}
	defer func() { _ = nightSet.Close() }()
	view, found := nightSet.FindView("Scene G15")
	if !found {
		stage.Close()
		return fmt.Errorf("startup game set has no Scene G15 view")
	}
	worldPoint, err := nightSet.StartPoint()
	if err != nil {
		stage.Close()
		return fmt.Errorf("read startup game position: %w", err)
	}
	gameClock, gameDay := scripts.NativeAdvanceClockFields(2, 1, 0)
	if *debug {
		log.Printf("game-time=day:%d clock:%d phase:0 source=NEW.FLT/advanceday", gameDay, gameClock)
	}
	propArchive, err := workspace.OpenPropArchive("DATA/HOUSE.PRP")
	if err != nil {
		stage.Close()
		return fmt.Errorf("open prop archive: %w", err)
	}
	defer propArchive.Close()
	avatarView, err := propArchive.View("avatar", "gossip")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve avatar gossip view: %w", err)
	}
	if len(avatarView.Frames) < 2 {
		stage.Close()
		return fmt.Errorf("avatar gossip view has %d degree rows, want 2", len(avatarView.Frames))
	}
	var avatarFrames [2]render.PuppetFrame
	var avatarResources [2]uint32
	for degree := range avatarFrames {
		resource, err := avatarView.FrameResource(degree, 0)
		if err != nil {
			stage.Close()
			return fmt.Errorf("resolve avatar gossip degree %d: %w", degree, err)
		}
		data, err := propArchive.Resource(resource)
		if err != nil {
			stage.Close()
			return fmt.Errorf("read avatar gossip frame %d: %w", resource, err)
		}
		avatarFrames[degree], err = render.DecodePuppetFrame(data)
		if err != nil {
			stage.Close()
			return fmt.Errorf("decode avatar gossip frame %d: %w", resource, err)
		}
		avatarResources[degree] = resource
	}
	if *debug {
		log.Printf("prop=avatar view=gossip degree-resources=%d,%d anchor=456,328 placement=panel-art-inference", avatarResources[0], avatarResources[1])
	}
	townActors, err := extraCast.ResolveLocations(nightSet, "town")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve startup town actors: %w", err)
	}
	leroyPosition, hasLeroy, err := nightSet.ResolveLocation("town.leroy1")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve G15 Leroy position: %w", err)
	}
	actorPoses := map[string]string{"leroy": "stand", "dog": "stand"}
	actorHeadings := map[string]int16{"leroy": 0, "dog": 32}
	actorTurnTargets := map[string]int16{"leroy": 0}
	actorTurnActive := false
	const leroyTurnRate int16 = 7
	const leroyWalkRate int16 = 3
	const (
		leroyInteractionIdle uint8 = iota
		leroyInteractionMoving
		leroyInteractionFacing
		leroyInteractionPuppetPending
		leroyInteractionPuppetSpeaking
		leroyInteractionPuppetChoices
		leroyInteractionReturning
	)
	leroyPhase, leroyInteractionStage := int16(0), leroyInteractionIdle
	var leroyWalk *scripts.NativeActorWalkJob
	var leroyReturnPosition [3]int16
	var leroyPuppet *render.Puppet
	var leroyPuppetTable assets.PuppetSpeechTable
	var leroyDialogue *engine.PuppetDialogue
	var leroyChoices, leroyActiveChoices []scripts.PuppetChoice
	var leroyChoiceAnswered = map[int32]bool{}
	var leroyChoicePressActive bool
	var leroyChoicePressEvent int32
	leroyChoicePressIndex, leroyChoiceOutline := -1, -1
	var leroyDialogueRepeats, leroyDialogueReturns, leroyDialogueSetsPhase bool
	var leroySkipDialogue bool
	var leroyConversationBase render.IndexedFrame
	var leroyChoiceBase render.IndexedFrame
	defer func() {
		if leroyDialogue != nil {
			_ = leroyDialogue.Close()
		}
		if leroyPuppet != nil {
			_ = leroyPuppet.Close()
		}
	}()
	loadWorldActors := func(point [3]int16) ([]render.WorldActorSprite, error) {
		actors := make([]render.WorldActorSprite, 0, 1)
		for _, actor := range gangCast.Actors {
			if !strings.EqualFold(actor.Name, "leroy") {
				continue
			}
			if !hasLeroy {
				return nil, fmt.Errorf("NITE.SET has no town.leroy1 coordinate")
			}
			actor.Position, actor.Located = leroyPosition, true
			sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["leroy"], 0, 1100, render.NativeActorViewAngle(leroyPosition, point, actorHeadings["leroy"]), 32)
			if err != nil {
				return nil, fmt.Errorf("load G15 actor %s: %w", actor.Name, err)
			}
			actors = append(actors, sprite)
		}
		for _, actor := range townActors {
			if gameClock != 3 || !strings.EqualFold(actor.Name, "dog") {
				continue
			}
			sprite, err := render.LoadCastActorFrame(workspace, extraCast, actor, actorPoses["dog"], 0, 880, render.NativeActorViewAngle(actor.Position, point, actorHeadings["dog"]), 32)
			if err != nil {
				return nil, fmt.Errorf("load G15 actor %s: %w", actor.Name, err)
			}
			actors = append(actors, sprite)
		}
		return actors, nil
	}
	if *debug {
		log.Printf("cast-location-records=set=NITE.SET selector=town count=%d", len(townActors))
		for _, actor := range townActors {
			log.Printf("cast-location name=%s point=%v script=%s", actor.Name, actor.Position, actor.Script)
		}
	}
	backgroundResource, found, err := nightSet.BackgroundResourceForDirection(view, worldPoint[2])
	if err != nil || !found {
		stage.Close()
		if err != nil {
			return fmt.Errorf("resolve startup game background: %w", err)
		}
		return fmt.Errorf("startup game set has no north-facing background for Scene G15")
	}
	backgroundData, err := nightSet.Resource(backgroundResource)
	if err != nil {
		stage.Close()
		return fmt.Errorf("read startup game background resource %d: %w", backgroundResource, err)
	}
	backgroundPixels, backgroundDecodeErr := render.DecodeMoviePixels(backgroundData, nil)
	if len(backgroundPixels.Pixels) == 0 {
		stage.Close()
		return fmt.Errorf("decode startup game background resource %d: %w", backgroundResource, backgroundDecodeErr)
	}
	if backgroundDecodeErr != nil && *debug {
		log.Printf("level=set=NITE.SET view=Scene G15 partial-frame: %v", backgroundDecodeErr)
	}
	backgroundFrame, err := render.StageFrame(&assets.Stage{Width: uint16(backgroundPixels.Width), Height: uint16(backgroundPixels.Height), PaletteRaw: nightSet.Palette()}, backgroundPixels.Pixels)
	if err != nil {
		stage.Close()
		return fmt.Errorf("render startup game background: %w", err)
	}
	worldActors, err := loadWorldActors(worldPoint)
	if err != nil {
		stage.Close()
		return err
	}
	worldBackground, projectedActors, err := render.CompositeWorldActors(backgroundFrame, worldPoint, worldActors)
	if err != nil {
		stage.Close()
		return fmt.Errorf("render G15 actors: %w", err)
	}
	composeMainPanel := func(worldBackground, panel render.IndexedFrame) (render.IndexedFrame, error) {
		frame, err := render.CompositeUnderlay(worldBackground, panel)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		degree := 1
		if gameClock == 3 {
			degree = 0
		}
		return render.CompositePuppetFrame(frame, avatarFrames[degree], image.Pt(456, 328))
	}
	stageFrame, err := composeMainPanel(worldBackground, frame)
	if err != nil {
		stage.Close()
		return fmt.Errorf("compose startup game scene: %w", err)
	}
	if *debug {
		log.Printf("level=set=NITE.SET view=%s ids=%d,%d direction=north frame-resource=%d size=%dx%d", view.Name[1:], view.SceneID, view.DirectionID, backgroundResource, backgroundFrame.Width, backgroundFrame.Height)
		for _, actor := range projectedActors {
			log.Printf("world-actor=%s depth=%d bounds=%d,%d,%d,%d", actor.Name, actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
		}
	}
	if *silent {
		if *debug {
			themeBank, err = audio.OpenSoundBank(workspace, "DATA/NIGHT.SND")
			if err != nil {
				stage.Close()
				return fmt.Errorf("open headless theme bank: %w", err)
			}
			theme, err := themeBank.LoadTheme("town.snd")
			if err != nil {
				stage.Close()
				return fmt.Errorf("decode headless town theme: %w", err)
			}
			log.Printf("theme-file=DATA/NIGHT.SND theme=%s events=%d voices=%d", theme.Name, len(theme.Events), len(theme.Tracks))
		}
		screenshotPath := filepath.Join(workspace.WorkDir, "redust-silent.png")
		if err := render.WritePNG(screenshotPath, stageFrame); err != nil {
			stage.Close()
			return fmt.Errorf("write silent startup screenshot: %w", err)
		}
		if *debug {
			log.Printf("silent screenshot=%s", screenshotPath)
		}
		if err := stage.Close(); err != nil {
			return fmt.Errorf("close initial stage: %w", err)
		}
		return nil
	}
	movieNames := []string{"MOVIES/INTRO.MOV", "MOVIES/INTRO2.MOV"}
	movieIndex := 0
	movieWarningCount := make([]int, len(movieNames))
	movieWarningSample := make([]string, len(movieNames))
	movie, err := render.OpenMovie(workspace, movieNames[movieIndex])
	if err != nil {
		stage.Close()
		return fmt.Errorf("open startup movie %s: %w", movieNames[movieIndex], err)
	}
	defer func() {
		if movie != nil {
			_ = movie.Close()
		}
	}()
	blackFrame, err := render.BlackFrame(frame.Width, frame.Height)
	if err != nil {
		stage.Close()
		return fmt.Errorf("create startup movie frame: %w", err)
	}
	playback, err := render.NewMoviePlayback(movie, blackFrame)
	if err != nil {
		stage.Close()
		return fmt.Errorf("start startup movie %s: %w", movieNames[movieIndex], err)
	}
	startMovieAudio := func(movie *render.Movie) (*ebitenaudio.Player, int, int, error) {
		resources, loopIndex := movie.SoundtrackResources()
		if len(resources) == 0 {
			return nil, 0, loopIndex, nil
		}
		tracks, events, indices := make([]audio.NativeSound, 0), make([]int, 0, len(resources)), make(map[uint32]int)
		for _, resource := range resources {
			track, ok := indices[resource]
			if !ok {
				data, err := movie.Resource(resource)
				if err != nil {
					return nil, 0, 0, fmt.Errorf("read movie soundtrack resource %d: %w", resource, err)
				}
				sound, err := audio.DecodeNativeSoundResource(data)
				if err != nil {
					return nil, 0, 0, fmt.Errorf("decode movie soundtrack resource %d: %w", resource, err)
				}
				track = len(tracks)
				indices[resource] = track
				tracks = append(tracks, sound)
			}
			events = append(events, track)
		}
		player, err := audio.NewNativePlaylist(audioContext, tracks, events, loopIndex)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("start movie soundtrack: %w", err)
		}
		return player, len(events), loopIndex, nil
	}
	movieAudio, movieAudioEvents, movieAudioLoop, err := startMovieAudio(movie)
	if err != nil {
		stage.Close()
		return err
	}
	defer func() {
		if movieAudio != nil {
			_ = movieAudio.Close()
		}
	}()
	if *debug {
		log.Printf("movie=%s frames=%d", movieNames[movieIndex], movie.FrameCount())
		if movieAudio != nil {
			log.Printf("movie-audio=%s events=%d loop=%d playing=%t", movieNames[movieIndex], movieAudioEvents, movieAudioLoop, movieAudio.IsPlaying())
		}
	}
	currentScene, currentPixels := 0, pixels
	currentFrame := stageFrame
	refreshWorldScene := func() error {
		if currentScene != 0 {
			return nil
		}
		actors, err := loadWorldActors(worldPoint)
		if err != nil {
			return err
		}
		worldBackground, projected, err := render.CompositeWorldActors(backgroundFrame, worldPoint, actors)
		if err != nil {
			return fmt.Errorf("refresh NITE actors: %w", err)
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return fmt.Errorf("refresh mainpanel: %w", err)
		}
		nextFrame, err := composeMainPanel(worldBackground, panel)
		if err != nil {
			return fmt.Errorf("compose refreshed NITE scene: %w", err)
		}
		worldActors, projectedActors = actors, projected
		stageFrame, currentFrame = nextFrame, nextFrame
		return nil
	}
	openLeroyPuppet := func() error {
		if leroyPuppet != nil {
			return nil
		}
		puppet, err := render.OpenPuppet(workspace, "PUPPETS/LEROY.PUP")
		if err != nil {
			return err
		}
		cache, err := workspace.OpenResourceCache("PUPPETS/LEROY.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		defer cache.Close()
		lease, err := cache.Acquire(88)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		scriptData, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = puppet.Close()
			return err
		}
		program, err := scripts.ParseProgram(scriptData)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		calls, err := scripts.PuppetSpeechCalls(program, "bysign", scripts.LookupOpcode("puppetclear"))
		if err != nil {
			_ = puppet.Close()
			return err
		}
		choices, err := scripts.PuppetBevelChoices(program, "bysign")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		table, err := workspace.OpenPuppetSpeechTable("PUPPETS/LEROY.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		dialogue, err := engine.NewPuppetDialogue(puppet, table.Entries, calls, audioContext)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		leroyPuppet, leroyPuppetTable, leroyDialogue, leroyChoices = puppet, table, dialogue, choices
		return nil
	}
	startLeroyBySign := func() error {
		if err := openLeroyPuppet(); err != nil {
			return fmt.Errorf("open Leroy dialogue: %w", err)
		}
		dialogueActors := make([]render.WorldActorSprite, 0, len(worldActors))
		for _, actor := range worldActors {
			if !strings.EqualFold(actor.Name, "leroy") {
				dialogueActors = append(dialogueActors, actor)
			}
		}
		dialogueBackground, _, err := render.CompositeWorldActors(backgroundFrame, worldPoint, dialogueActors)
		if err != nil {
			return fmt.Errorf("hide Leroy world sprite for dialogue: %w", err)
		}
		dialoguePanel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return fmt.Errorf("render dialogue panel: %w", err)
		}
		leroyConversationBase, err = composeMainPanel(dialogueBackground, dialoguePanel)
		if err != nil {
			return fmt.Errorf("compose Leroy dialogue background: %w", err)
		}
		palette, err := leroyPuppet.Palette()
		if err != nil {
			return fmt.Errorf("load Leroy PUP CLUT: %w", err)
		}
		leroyConversationBase.Palette = palette
		frame, err := leroyDialogue.Start(leroyConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			return fmt.Errorf("start Leroy bysign dialogue: %w", err)
		}
		currentFrame, stageFrame = frame, frame
		leroyDialogueRepeats, leroyDialogueReturns, leroyDialogueSetsPhase = true, false, false
		leroyInteractionStage = leroyInteractionPuppetSpeaking
		if *debug {
			log.Printf("puppet=leroy script=bysign lines=%d choices=%d", 3, len(leroyChoices))
		}
		return nil
	}
	drawLeroyChoices := func(outline int) error {
		labels := make([]string, len(leroyActiveChoices))
		for index, choice := range leroyActiveChoices {
			labels[index] = choice.Text
		}
		choiceBackground := leroyChoiceBase
		frame, err := leroyPuppet.ChoiceFrame(choiceBackground, leroyPuppetTable.PanelResource, labels)
		if err != nil {
			return fmt.Errorf("render Leroy choice panel: %w", err)
		}
		if outline >= 0 {
			frame, err = render.DrawNativePuppetChoiceBevel(frame, outline)
			if err != nil {
				return fmt.Errorf("render Leroy choice bevel: %w", err)
			}
		}
		currentFrame, stageFrame = frame, frame
		leroyChoiceOutline = outline
		return nil
	}
	showLeroyChoices := func() error {
		leroyActiveChoices = leroyActiveChoices[:0]
		for _, choice := range leroyChoices {
			if !leroyChoiceAnswered[choice.EventID] {
				leroyActiveChoices = append(leroyActiveChoices, choice)
			}
		}
		if len(leroyActiveChoices) == 0 {
			return fmt.Errorf("Leroy bysign reached its event wait with no enabled choices")
		}
		if leroyInteractionStage != leroyInteractionPuppetChoices {
			leroyChoiceBase, leroyChoicePressActive = currentFrame, false
			leroyChoicePressIndex, leroyChoiceOutline = -1, -1
		}
		if err := drawLeroyChoices(-1); err != nil {
			return err
		}
		leroyInteractionStage = leroyInteractionPuppetChoices
		return nil
	}
	returnLeroyToStar := func() error {
		if leroyPosition == leroyReturnPosition {
			leroyInteractionStage = leroyInteractionIdle
			return nil
		}
		heading := render.NativeActorHeadingToPoint(leroyPosition, leroyReturnPosition)
		walk := scripts.NewNativeActorWalkJob(leroyPosition, leroyReturnPosition, heading, leroyWalkRate)
		leroyWalk, leroyInteractionStage = &walk, leroyInteractionReturning
		return nil
	}
	startLeroyResponse := func(event int32) error {
		calls, found := scripts.LeroyBySignResponseCalls(event)
		if !found {
			return fmt.Errorf("Leroy bysign returned unsupported event %d", event)
		}
		flow, found := scripts.LeroyBySignResponseTransition(event, gameDay)
		if !found {
			return fmt.Errorf("Leroy bysign event %d has no verified response transition", event)
		}
		if err := leroyDialogue.Close(); err != nil {
			return err
		}
		dialogue, err := engine.NewPuppetDialogue(leroyPuppet, leroyPuppetTable.Entries, calls, audioContext)
		if err != nil {
			return err
		}
		leroyDialogue = dialogue
		frame, err := leroyDialogue.Start(leroyConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			return err
		}
		currentFrame, stageFrame = frame, frame
		leroyChoiceAnswered[event] = true
		leroyDialogueRepeats, leroyDialogueReturns, leroyDialogueSetsPhase = flow.Repeats, flow.Returns, flow.SetsPhase
		leroyInteractionStage = leroyInteractionPuppetSpeaking
		if *debug {
			log.Printf("puppet=leroy event=%d response-lines=%d", event, len(calls))
		}
		return nil
	}
	finishLeroyDialogue := func() error {
		if leroyDialogueSetsPhase {
			leroyPhase = 1
		}
		if leroyDialogueRepeats {
			return showLeroyChoices()
		}
		if leroyDialogueReturns {
			return returnLeroyToStar()
		}
		return showLeroyChoices()
	}
	runNativeScheduler := func(serviceAmbient bool) (bool, error) {
		displayChanged := false
		status, err := nativeLoops.PassWhere(func(loop scripts.ScriptLoop) (uint16, error) {
			switch loop.Callback {
			case "toidle", "leroyidle":
				dx, dy, dz := int(leroyPosition[0])-int(worldPoint[0]), int(leroyPosition[1])-int(worldPoint[1]), int(leroyPosition[2])-int(worldPoint[2])
				step, found := scripts.LeroyIdleStep(loop.Callback, dx*dx+dy*dy+dz*dz < 384*384, true, leroyPhase, &nativeRandom)
				if !found {
					return 0, fmt.Errorf("unknown Leroy idle callback %q", loop.Callback)
				}
				actorPoses["leroy"], loop.Callback, loop.Remaining = step.Pose, step.Callback, step.Remaining
				if step.TurnToCamera {
					actorTurnTargets["leroy"] = render.NativeActorHeadingToCamera(leroyPosition, worldPoint)
					actorTurnActive = actorHeadings["leroy"] != actorTurnTargets["leroy"]
				}
				if step.TurnBy != 0 {
					actorHeadings["leroy"] = int16((int(actorHeadings["leroy"]) + int(step.TurnBy) + 256) % 256)
				}
				displayChanged = displayChanged || currentScene == 0
				if *debug {
					log.Printf("actor=leroy pose=%s next=%s ticks=%d heading=%d", step.Pose, step.Callback, step.Remaining, actorHeadings["leroy"])
				}
			case "nightfxs":
				cue, found := scripts.NightWildlifeCue(currentThemeName, &nativeRandom)
				if found {
					played, err := themeBank.PlayAtVolume(audioContext, cue.Name, 255)
					if err != nil {
						return 0, fmt.Errorf("play NITE wildlife cue %s: %w", cue.Name, err)
					}
					if *debug {
						log.Printf("ambient-cue=%s pan=%d played=%t", cue.Name, cue.Pan, played)
					}
				}
				loop.Remaining = 2
			case "lookright", "doleft", "lookleft", "doright":
				step, found := scripts.DogIdleStep(loop.Callback, &nativeRandom)
				if !found {
					return 0, fmt.Errorf("unknown dog idle callback %q", loop.Callback)
				}
				actorPoses["dog"], loop.Callback, loop.Remaining = step.Pose, step.Callback, step.Remaining
				displayChanged = displayChanged || currentScene == 0
				if *debug {
					log.Printf("actor=dog pose=%s next=%s ticks=%d", step.Pose, step.Callback, step.Remaining)
				}
			default:
				return 0, fmt.Errorf("unknown native loop callback %q", loop.Callback)
			}
			return nativeLoops.Register(loop), nil
		}, func(loop scripts.ScriptLoop) bool { return (loop.Callback == "nightfxs") == serviceAmbient })
		if err != nil {
			return false, err
		}
		if status != 0 {
			return false, fmt.Errorf("native scene scheduler returned status %#x", status)
		}
		if !serviceAmbient && actorTurnActive {
			actorHeadings["leroy"] = scripts.NativeTurnStep(actorHeadings["leroy"], actorTurnTargets["leroy"], leroyTurnRate)
			actorTurnActive = actorHeadings["leroy"] != actorTurnTargets["leroy"]
			displayChanged = displayChanged || currentScene == 0
			if *debug {
				log.Printf("actor=leroy turn heading=%d target=%d active=%t", actorHeadings["leroy"], actorTurnTargets["leroy"], actorTurnActive)
			}
			if !actorTurnActive && leroyInteractionStage == leroyInteractionFacing {
				leroyInteractionStage = leroyInteractionPuppetPending
				if *debug {
					log.Printf("actor=leroy interaction=puppet pending file=PUPPETS/LEROY.PUP")
				}
			}
		}
		if leroyWalk != nil {
			previousPosition, previousHeading := leroyPosition, actorHeadings["leroy"]
			var walking bool
			leroyPosition, actorHeadings["leroy"], walking = leroyWalk.Pass(leroyPosition, actorHeadings["leroy"], leroyTurnRate)
			displayChanged = displayChanged || leroyPosition != previousPosition || actorHeadings["leroy"] != previousHeading
			if *debug && (leroyPosition != previousPosition || actorHeadings["leroy"] != previousHeading) {
				log.Printf("actor=leroy walk point=%v heading=%d active=%t", leroyPosition, actorHeadings["leroy"], walking)
			}
			if !walking {
				leroyWalk = nil
				if leroyInteractionStage == leroyInteractionMoving {
					currentDegree, found := render.NativeCurrentDegree(worldPoint[2])
					if !found {
						return false, fmt.Errorf("NITE.SET orientation %d has no native currentdeg", worldPoint[2])
					}
					actorTurnTargets["leroy"] = int16((int(currentDegree) + 128) % 256)
					actorTurnActive = actorHeadings["leroy"] != actorTurnTargets["leroy"]
					leroyInteractionStage = leroyInteractionFacing
					if !actorTurnActive {
						leroyInteractionStage = leroyInteractionPuppetPending
					}
				} else if leroyInteractionStage == leroyInteractionReturning {
					leroyInteractionStage = leroyInteractionIdle
				}
			}
		}
		if displayChanged {
			if err := refreshWorldScene(); err != nil {
				return false, err
			}
		}
		if leroyInteractionStage == leroyInteractionPuppetPending {
			if err := startLeroyBySign(); err != nil {
				return false, err
			}
			return true, nil
		}
		if leroyInteractionStage == leroyInteractionPuppetSpeaking {
			if leroyDialogue == nil {
				return false, fmt.Errorf("Leroy dialogue state is missing its puppet player")
			}
			frameTick := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			var frame render.IndexedFrame
			var changed bool
			var err error
			if leroySkipDialogue {
				frame, changed, err = leroyDialogue.Skip()
				leroySkipDialogue = false
			} else {
				frame, changed, err = leroyDialogue.Update(frameTick)
			}
			if err != nil {
				return false, fmt.Errorf("advance Leroy dialogue: %w", err)
			}
			if changed {
				currentFrame, stageFrame = frame, frame
				if !leroyDialogue.Active() {
					if err := finishLeroyDialogue(); err != nil {
						return false, fmt.Errorf("finish Leroy dialogue: %w", err)
					}
				}
				return true, nil
			}
			if displayChanged {
				frame, err = leroyDialogue.Frame()
				if err != nil {
					return false, fmt.Errorf("refresh Leroy dialogue frame: %w", err)
				}
				currentFrame, stageFrame = frame, frame
				return true, nil
			}
		}
		if displayChanged && leroyInteractionStage == leroyInteractionPuppetChoices {
			if err := showLeroyChoices(); err != nil {
				return false, fmt.Errorf("refresh Leroy choice panel: %w", err)
			}
			return true, nil
		}
		if leroyInteractionStage == leroyInteractionReturning && leroyWalk == nil {
			leroyInteractionStage = leroyInteractionIdle
		}
		return displayChanged, nil
	}
	startSceneMovie := func(name string) error {
		if movieAudio != nil {
			if err := movieAudio.Close(); err != nil {
				return fmt.Errorf("stop current movie soundtrack: %w", err)
			}
			movieAudio = nil
		}
		if movie != nil {
			if err := movie.Close(); err != nil {
				return fmt.Errorf("close current movie: %w", err)
			}
		}
		movieNames, movieIndex = []string{name}, 0
		movieWarningCount, movieWarningSample = []int{0}, []string{""}
		movie, err = render.OpenMovie(workspace, name)
		if err != nil {
			return fmt.Errorf("open scene movie %s: %w", name, err)
		}
		playback, err = render.NewMoviePlayback(movie, currentFrame)
		if err != nil {
			return fmt.Errorf("start scene movie %s: %w", name, err)
		}
		movieAudio, movieAudioEvents, movieAudioLoop, err = startMovieAudio(movie)
		if err != nil {
			return err
		}
		if *debug {
			log.Printf("movie=%s frames=%d source=SET object-click", name, movie.FrameCount())
		}
		return nil
	}
	var pendingMovement assets.SceneMove
	var transition *render.BarndoorEffect
	runErr := engine.Run(playback.CurrentFrame(), func() (render.IndexedFrame, bool, error) {
		if playback == nil && transition == nil {
			changed, err := runNativeScheduler(false)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if changed {
				return currentFrame, true, nil
			}
		}
		if playback == nil && transition == nil && currentScene == 0 && pendingMovement != 0 {
			movement := pendingMovement
			pendingMovement = 0
			nextPoint, transitionResource, found, err := nightSet.MovePoint(worldPoint, movement)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("move in NITE.SET: %w", err)
			}
			if !found {
				if *debug {
					log.Printf("level-move blocked movement=%d point=%v", movement, worldPoint)
				}
				return render.IndexedFrame{}, false, nil
			}
			viewPoint := assets.SetView{DirectionID: uint16(nextPoint[0]), SceneID: uint16(nextPoint[1])}
			frameResource, hasView, err := nightSet.BackgroundResourceForDirection(viewPoint, nextPoint[2])
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("resolve NITE.SET directional background after movement: %w", err)
			}
			if !hasView {
				frameResource = transitionResource
			}
			backgroundData, err := nightSet.Resource(frameResource)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("load NITE.SET background %d: %w", frameResource, err)
			}
			backgroundPixels, decodeErr := render.DecodeMoviePixels(backgroundData, nil)
			if len(backgroundPixels.Pixels) == 0 {
				return render.IndexedFrame{}, false, fmt.Errorf("decode NITE.SET background %d: %w", frameResource, decodeErr)
			}
			if decodeErr != nil && *debug {
				log.Printf("level-resource=%d partial-frame: %v", frameResource, decodeErr)
			}
			nextBackground, err := render.StageFrame(&assets.Stage{Width: uint16(backgroundPixels.Width), Height: uint16(backgroundPixels.Height), PaletteRaw: nightSet.Palette()}, backgroundPixels.Pixels)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("render NITE.SET background %d: %w", frameResource, err)
			}
			nextActors, err := loadWorldActors(nextPoint)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			nextWorldBackground, nextProjectedActors, err := render.CompositeWorldActors(nextBackground, nextPoint, nextActors)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("render NITE.SET actors: %w", err)
			}
			overlay, err := render.StageFrame(stage, currentPixels.Pixels)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("render stage scene %d: %w", currentScene, err)
			}
			nextFrame, err := composeMainPanel(nextWorldBackground, overlay)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("compose moved game background: %w", err)
			}
			worldPoint, backgroundFrame, stageFrame, currentFrame = nextPoint, nextBackground, nextFrame, nextFrame
			worldActors, projectedActors = nextActors, nextProjectedActors
			if *debug {
				log.Printf("level-move=%d point=%v transition-resource=%d frame-resource=%d", movement, worldPoint, transitionResource, frameResource)
				for _, actor := range projectedActors {
					log.Printf("world-actor=%s depth=%d bounds=%d,%d,%d,%d", actor.Name, actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
				}
			}
			return nextFrame, true, nil
		}
		if playback == nil {
			if transition == nil {
				return render.IndexedFrame{}, false, nil
			}
			transitionFrame, changed, done := transition.Update()
			if done {
				currentFrame, transition = transition.TargetFrame(), nil
			}
			return transitionFrame, changed, nil
		}
		movieFrame, changed, done, err := playback.Update()
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("advance startup movie %s: %w", movieNames[movieIndex], err)
		}
		if warning := playback.TakeDecodeWarning(); warning != nil {
			movieWarningCount[movieIndex]++
			if movieWarningSample[movieIndex] == "" {
				movieWarningSample[movieIndex] = warning.Error()
			}
		}
		if !done {
			return movieFrame, changed, nil
		}
		if *debug && movieWarningCount[movieIndex] > 0 {
			log.Printf("movie=%s decode-warnings=%d first=%s", movieNames[movieIndex], movieWarningCount[movieIndex], movieWarningSample[movieIndex])
		}
		if movieAudio != nil {
			if err := movieAudio.Close(); err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("stop startup soundtrack %s: %w", movieNames[movieIndex], err)
			}
			movieAudio = nil
		}
		if err := movie.Close(); err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("close startup movie %s: %w", movieNames[movieIndex], err)
		}
		movie = nil
		if movieIndex+1 < len(movieNames) {
			movieIndex++
			movie, err = render.OpenMovie(workspace, movieNames[movieIndex])
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("open startup movie %s: %w", movieNames[movieIndex], err)
			}
			playback, err = render.NewMoviePlayback(movie, movieFrame)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("start startup movie %s: %w", movieNames[movieIndex], err)
			}
			movieAudio, movieAudioEvents, movieAudioLoop, err = startMovieAudio(movie)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if *debug {
				log.Printf("movie=%s frames=%d", movieNames[movieIndex], movie.FrameCount())
				if movieAudio != nil {
					log.Printf("movie-audio=%s events=%d loop=%d playing=%t", movieNames[movieIndex], movieAudioEvents, movieAudioLoop, movieAudio.IsPlaying())
				}
			}
			return playback.CurrentFrame(), true, nil
		}
		playback = nil
		currentFrame = stageFrame
		if *debug {
			log.Printf("startup movies complete; scene=%s", stage.Scenes[currentScene].Name[1:])
		}
		if themePlayer == nil {
			themeBank, err = audio.OpenSoundBank(workspace, "DATA/NIGHT.SND")
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("open startup theme bank: %w", err)
			}
			theme, err := themeBank.LoadTheme("town.snd")
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("load startup town theme: %w", err)
			}
			themePlayer, err = theme.Play(audioContext)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("start startup town theme: %w", err)
			}
			currentThemeName = theme.FirstVoiceName()
			tick := scripts.NativeTickMilliseconds()
			nativeRandom = scripts.NewNativeRandom(scripts.NativeRandomSeed(tick))
			if gameClock == 3 {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "leroy", Callback: "leroyidle", Remaining: 20}); status != 0 {
					return render.IndexedFrame{}, false, fmt.Errorf("register Leroy idle loop returned status %#x", status)
				}
				step, found := scripts.DogIdleStep("doright", &nativeRandom)
				if found {
					actorPoses["dog"] = step.Pose
					if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "dog", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
						return render.IndexedFrame{}, false, fmt.Errorf("register dog idle loop returned status %#x", status)
					}
				}
			}
			if currentThemeName == "nightwind3" {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 1, Owner: "scene g14", Callback: "nightfxs", Remaining: 2}); status != 0 {
					return render.IndexedFrame{}, false, fmt.Errorf("register NITE nightfxs loop returned status %#x", status)
				}
			}
			if *debug {
				log.Printf("track-file=DATA/NIGHT.SND theme=%s events=%d voices=%d channel-name=%s playing=%t", theme.Name, len(theme.Events), len(theme.Tracks), currentThemeName, themePlayer.IsPlaying())
			}
		}
		if err := soundBank.Play(audioContext, "pageturn", 4); err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("play intro transition sound: %w", err)
		}
		if *debug {
			log.Printf("sound=pageturn active-players=%d", soundBank.ActivePlayers())
		}
		return stageFrame, true, nil
	}, func(key ebiten.Key) {
		if leroyInteractionStage == leroyInteractionPuppetSpeaking {
			if key == ebiten.KeySpace || key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				leroySkipDialogue = true
			}
			return
		}
		if leroyInteractionStage == leroyInteractionPuppetChoices {
			if key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				if err := returnLeroyToStar(); err != nil && *debug {
					log.Printf("cancel Leroy choice: %v", err)
				}
			}
			return
		}
		if playback == nil {
			if currentScene == 0 && transition == nil {
				switch key {
				case ebiten.KeyArrowUp:
					pendingMovement = assets.SceneMoveStraight
				case ebiten.KeyArrowLeft:
					pendingMovement = assets.SceneMoveLeft
				case ebiten.KeyArrowRight:
					pendingMovement = assets.SceneMoveRight
				}
			}
			return
		}
		if key != ebiten.KeyEscape && key != ebiten.KeySpace && key != ebiten.KeyQ && key != ebiten.KeyPeriod {
			return
		}
		playback.Skip()
		if *debug {
			keyName := "q"
			switch key {
			case ebiten.KeyEscape:
				keyName = "escape"
			case ebiten.KeySpace:
				keyName = "space"
			case ebiten.KeyPeriod:
				keyName = "period"
			}
			log.Printf("movie=%s skip-key=%s", movieNames[movieIndex], keyName)
		}
	}, func(mouseEvent engine.MouseEvent) (render.IndexedFrame, bool, error) {
		point := mouseEvent.Point
		if playback != nil || transition != nil {
			return render.IndexedFrame{}, false, nil
		}
		if leroyInteractionStage != leroyInteractionIdle && leroyInteractionStage != leroyInteractionPuppetChoices {
			return currentFrame, false, nil
		}
		if leroyInteractionStage == leroyInteractionPuppetChoices {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			event, found := scripts.NativePuppetChoiceAt(point, leroyActiveChoices)
			if !found {
				return currentFrame, false, nil
			}
			leroyChoicePressActive, leroyChoicePressEvent = true, event
			for index, choice := range leroyActiveChoices {
				if choice.EventID == event {
					leroyChoicePressIndex = index
					break
				}
			}
			return currentFrame, false, nil
		}
		handler, hit, err := stage.HitTestSceneHandler(currentScene, point)
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("hit test scene %d: %w", currentScene, err)
		}
		if *debug {
			handlerName := ""
			if hit {
				handlerName = string(handler.Name[1:])
			}
			log.Printf("mouse scene=%s point=%d,%d hit=%t handler=%s", stage.Scenes[currentScene].Name[1:], int16(point>>16), int16(point), hit, handlerName)
			if !hit {
				regions, err := stage.SceneHandlers(currentScene)
				if err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("read scene %d handler table: %w", currentScene, err)
				}
				for _, region := range regions {
					log.Printf("handler scene=%s name=%s rect=%d,%d,%d,%d script=%d", stage.Scenes[currentScene].Name[1:], region.Name[1:], region.Left, region.Top, region.Right, region.Bottom, region.ScriptResource)
				}
			}
		}
		if !hit && currentScene == 0 {
			if strings.EqualFold(string(view.Name[1:]), "Scene G15") {
				if action, found := scripts.NiteNorthObjectAction(worldPoint[2], point, gameClock); found {
					if *debug {
						log.Printf("world-object=%s movie=%s", action.Object, action.Movie)
					}
					if err := startSceneMovie(action.Movie); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
			}
			if actorName, found := render.HitTestWorldActors(projectedActors, image.Pt(int(int16(point>>16)), int(int16(point)))); found {
				if *debug {
					log.Printf("world-actor-hit=%s", actorName)
				}
				if sceneName, handlesClick := scripts.CastActorMouseDownScene(actorName); handlesClick {
					nextView, found := nightSet.FindView(sceneName)
					if !found {
						return render.IndexedFrame{}, false, fmt.Errorf("NITE.SET has no view %q", sceneName)
					}
					nextPoint := [3]int16{int16(nextView.DirectionID), int16(nextView.SceneID), worldPoint[2]}
					frameResource, found, err := nightSet.BackgroundResourceForDirection(nextView, nextPoint[2])
					if err != nil || !found {
						if err != nil {
							return render.IndexedFrame{}, false, fmt.Errorf("resolve %s background: %w", sceneName, err)
						}
						return render.IndexedFrame{}, false, fmt.Errorf("%s has no background for direction %d", sceneName, nextPoint[2])
					}
					backgroundData, err := nightSet.Resource(frameResource)
					if err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("read %s background resource %d: %w", sceneName, frameResource, err)
					}
					backgroundPixels, decodeErr := render.DecodeMoviePixels(backgroundData, nil)
					if len(backgroundPixels.Pixels) == 0 {
						return render.IndexedFrame{}, false, fmt.Errorf("decode %s background resource %d: %w", sceneName, frameResource, decodeErr)
					}
					if decodeErr != nil && *debug {
						log.Printf("scene=%s resource=%d partial-frame: %v", sceneName, frameResource, decodeErr)
					}
					nextBackground, err := render.StageFrame(&assets.Stage{Width: uint16(backgroundPixels.Width), Height: uint16(backgroundPixels.Height), PaletteRaw: nightSet.Palette()}, backgroundPixels.Pixels)
					if err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("render %s background: %w", sceneName, err)
					}
					nextActors, err := loadWorldActors(nextPoint)
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					nextWorldBackground, nextProjectedActors, err := render.CompositeWorldActors(nextBackground, nextPoint, nextActors)
					if err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("render %s actors: %w", sceneName, err)
					}
					panel, err := render.StageFrame(stage, currentPixels.Pixels)
					if err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("render mainpanel over %s: %w", sceneName, err)
					}
					nextFrame, err := composeMainPanel(nextWorldBackground, panel)
					if err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("compose %s scene: %w", sceneName, err)
					}
					view, worldPoint, backgroundFrame = nextView, nextPoint, nextBackground
					worldActors, projectedActors = nextActors, nextProjectedActors
					stageFrame, currentFrame = nextFrame, nextFrame
					if *debug {
						log.Printf("scene=%s direction=%d frame-resource=%d", view.Name[1:], worldPoint[2], frameResource)
						for _, actor := range projectedActors {
							log.Printf("world-actor=%s depth=%d bounds=%d,%d,%d,%d", actor.Name, actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
						}
					}
					return nextFrame, true, nil
				}
				if strings.EqualFold(actorName, "Leroy") && strings.EqualFold(string(view.Name[1:]), "Scene G15") && leroyInteractionStage == leroyInteractionIdle {
					playerWorld := render.NativeActorCameraPosition(worldPoint)
					playerPosition := [3]int16{int16(playerWorld[0]), int16(playerWorld[1]), int16(playerWorld[2])}
					distance := scripts.NativeActorDistance2D(leroyPosition, playerPosition)
					if scripts.LeroyMouseDownAction(gameDay, distance, 512) {
						if !scripts.NativeWalktopuppetAxisAligned(leroyPosition, playerPosition) {
							if *debug {
								log.Printf("actor=leroy walktopuppet=blocked-axis-alignment actor=%v player=%v", leroyPosition, playerPosition)
							}
							return currentFrame, true, nil
						}
						currentDegree, found := render.NativeCurrentDegree(worldPoint[2])
						if !found {
							return render.IndexedFrame{}, false, fmt.Errorf("NITE.SET orientation %d has no native currentdeg", worldPoint[2])
						}
						vector, found := render.NativeDirectionVector(currentDegree, 32)
						if !found {
							return render.IndexedFrame{}, false, fmt.Errorf("NITE.SET currentdeg %d has no native cardinal vector", currentDegree)
						}
						destination := [3]int16{playerPosition[0] + vector[0], playerPosition[1] + vector[1], 0}
						routeHeading := render.NativeActorHeadingToPoint(leroyPosition, destination)
						leroyReturnPosition = leroyPosition
						walk := scripts.NewNativeActorWalkJob(leroyPosition, destination, routeHeading, leroyWalkRate)
						leroyWalk, actorTurnActive, leroyInteractionStage = &walk, false, leroyInteractionMoving
						actorPoses["leroy"] = "stand"
						nativeLoops.Stop(2, "leroy")
						if *debug {
							log.Printf("actor=leroy walktopuppet distance=%d destination=%v heading=%d", distance, destination, routeHeading)
						}
						return currentFrame, true, nil
					}
				}
			}
		}
		if !hit {
			return render.IndexedFrame{}, false, nil
		}
		lease, err := stage.AcquireResource(handler.ScriptResource)
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("load button script resource %d: %w", handler.ScriptResource, err)
		}
		scriptBytes, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("read button script resource %d: %w", handler.ScriptResource, err)
		}
		program, err := scripts.ParseProgram(scriptBytes)
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("parse button script resource %d: %w", handler.ScriptResource, err)
		}
		action, found, err := scripts.MouseDownFlatAction(program)
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("run button script resource %d: %w", handler.ScriptResource, err)
		}
		if !found {
			if *debug {
				recordKinds := make([]uint16, len(program.Records))
				for i, record := range program.Records {
					recordKinds[i] = record.Kind
				}
				log.Printf("button script resource=%d record-kinds=%v", handler.ScriptResource, recordKinds)
			}
			return render.IndexedFrame{}, false, nil
		}
		target := action.FlatTarget
		if target < 0 || target >= len(stage.Scenes) {
			return render.IndexedFrame{}, false, fmt.Errorf("button script resource %d selects stage scene %d", handler.ScriptResource, target)
		}
		frameLease, err := stage.AcquireSceneFrameResource(target)
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("load stage scene %d: %w", target, err)
		}
		data, readErr := frameLease.Bytes()
		closeErr := frameLease.Close()
		if readErr != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("read stage scene %d: %w", target, readErr)
		}
		if closeErr != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("release stage scene %d: %w", target, closeErr)
		}
		nextPixels, decodeErr := render.DecodeMoviePixels(data, currentPixels.Pixels)
		if decodeErr != nil && len(nextPixels.Pixels) == 0 {
			return render.IndexedFrame{}, false, fmt.Errorf("decode stage scene %d: %w", target, decodeErr)
		}
		if decodeErr != nil && *debug {
			log.Printf("scene=%s partial-frame: %v", stage.Scenes[target].Name[1:], decodeErr)
		}
		nextFrame, err := render.StageFrame(stage, nextPixels.Pixels)
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("render stage scene %d: %w", target, err)
		}
		if target == 0 {
			worldBackground, visibleActors, actorErr := render.CompositeWorldActors(backgroundFrame, worldPoint, worldActors)
			if actorErr != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("render returned NITE actors: %w", actorErr)
			}
			projectedActors = visibleActors
			nextFrame, err = composeMainPanel(worldBackground, nextFrame)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("compose startup game background: %w", err)
			}
		}
		currentScene, currentPixels = target, nextPixels
		if action.VisualEffect != 0 {
			if _, err := runNativeScheduler(true); err != nil {
				return render.IndexedFrame{}, false, err
			}
			effectName := ""
			switch action.VisualEffect {
			case scripts.LookupOpcode("barndooropen"):
				effectName = "barndooropen"
				transition, err = render.NewBarndoorOpen(currentFrame, nextFrame, action.Duration)
			case scripts.LookupOpcode("barndoorclose"):
				effectName = "barndoorclose"
				transition, err = render.NewBarndoorClose(currentFrame, nextFrame, action.Duration)
			default:
				return render.IndexedFrame{}, false, fmt.Errorf("button script resource %d uses unsupported visual effect %d", handler.ScriptResource, action.VisualEffect)
			}
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("start %s transition: %w", effectName, err)
			}
			if err := soundBank.Play(audioContext, "pageturn", 4); err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("play page-turn sound: %w", err)
			}
			if *debug {
				log.Printf("visualeffect=%s duration=%d", effectName, action.Duration)
				log.Printf("sound=pageturn active-players=%d", soundBank.ActivePlayers())
			}
			return transition.CurrentFrame(), true, nil
		}
		currentFrame = nextFrame
		if *debug {
			log.Printf("scene transition=%s resource=%d", stage.Scenes[target].Name[1:], stage.Scenes[target].Fields[1])
		}
		return nextFrame, true, nil
	}, func(state engine.MouseState) (render.IndexedFrame, bool, error) {
		if leroyInteractionStage != leroyInteractionPuppetChoices || !leroyChoicePressActive {
			return currentFrame, false, nil
		}
		event, found := scripts.NativePuppetChoiceAt(state.Point, leroyActiveChoices)
		outline := -1
		if found && event == leroyChoicePressEvent {
			outline = leroyChoicePressIndex
		}
		if state.LeftDown {
			if outline == leroyChoiceOutline {
				return currentFrame, false, nil
			}
			if err := drawLeroyChoices(outline); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		}
		if state.LeftReleased {
			selected := outline >= 0 && outline == leroyChoicePressIndex
			selectedEvent := leroyChoicePressEvent
			leroyChoicePressActive, leroyChoicePressIndex = false, -1
			if selected {
				if err := startLeroyResponse(selectedEvent); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("run Leroy choice %d: %w", selectedEvent, err)
				}
				return currentFrame, true, nil
			}
			if leroyChoiceOutline >= 0 {
				if err := drawLeroyChoices(-1); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			return currentFrame, false, nil
		}
		return currentFrame, false, nil
	})
	soundCloseErr := soundBank.Close()
	closeErr := stage.Close()
	if runErr != nil {
		return runErr
	}
	if soundCloseErr != nil {
		return fmt.Errorf("close startup sound bank: %w", soundCloseErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close initial stage: %w", closeErr)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
