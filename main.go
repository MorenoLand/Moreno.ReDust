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

func loadInventoryFrame(archive *assets.PropArchive, propName, viewName string, angle int16) (render.PuppetFrame, error) {
	info, err := archive.FrameInfo(propName, viewName, 0, angle)
	if err != nil {
		return render.PuppetFrame{}, err
	}
	data, err := archive.Resource(info.Resource)
	if err != nil {
		return render.PuppetFrame{}, err
	}
	return render.DecodePuppetFrame(data)
}

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
	activeSet, activeSetName, activeSetOwned := nightSet, "town", false
	defer func() {
		if activeSetOwned && activeSet != nil {
			_ = activeSet.Close()
		}
	}()
	var doorOwner string
	townReturnScene := ""
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
	gameClock, gameDay, phase := scripts.NativeAdvanceClockFields(2, 1, 0)
	gamePhase, dogVisibleState := int16(phase), gameDay == 1
	if *debug {
		log.Printf("game-time=day:%d clock:%d phase:%d source=NEW.FLT/advanceday", gameDay, gameClock, gamePhase)
	}
	propArchive, err := workspace.OpenPropArchive("DATA/HOUSE.PRP")
	if err != nil {
		stage.Close()
		return fmt.Errorf("open prop archive: %w", err)
	}
	defer propArchive.Close()
	inventoryArchive, err := workspace.OpenPropArchive("DATA/INVEN.PRP")
	if err != nil {
		stage.Close()
		return fmt.Errorf("open inventory prop archive: %w", err)
	}
	defer inventoryArchive.Close()
	bonePosition, hasBone, err := nightSet.ResolveLocation("town.bone")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve NITE.SET Bone position: %w", err)
	}
	if !hasBone {
		stage.Close()
		return fmt.Errorf("NITE.SET has no town.bone coordinate")
	}
	boneSmallView, err := inventoryArchive.View("Bone", "small")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Bone small view: %w", err)
	}
	boneLargeView, err := inventoryArchive.View("Bone", "large")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Bone large view: %w", err)
	}
	inventoryLargeFrames := make(map[string]render.PuppetFrame, 5)
	for _, item := range []struct {
		name  string
		prop  string
		angle int16
	}{{"badge", "Badge", 0}, {"gun", "Gun", 0}, {"boots", "Boots", 0}, {"ring", "Ring", 0}, {"bone", "Bone", 32}} {
		frame, err := loadInventoryFrame(inventoryArchive, item.prop, "large", item.angle)
		if err != nil {
			stage.Close()
			return fmt.Errorf("load %s large inventory frame: %w", item.prop, err)
		}
		inventoryLargeFrames[item.name] = frame
	}
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
	helpPosition, hasHelp, err := nightSet.ResolveLocation("town.help")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve G15 Help position: %w", err)
	}
	jonesStartPosition, hasJonesStart, err := nightSet.ResolveLocation("town.jones1")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Jones start position: %w", err)
	}
	jonesTargetPosition, hasJonesTarget, err := nightSet.ResolveLocation("town.jones2")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Jones bar position: %w", err)
	}
	buickPosition, hasBuick, err := nightSet.ResolveLocation("town.blood1")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Town Buick position: %w", err)
	}
	mariePosition, hasMarie, err := nightSet.ResolveLocation("town.jones2")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Town Marie position: %w", err)
	}
	buickSecondPosition, hasBuickSecond, err := nightSet.ResolveLocation("town.blood2")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Town Buick idle position: %w", err)
	}
	marieSecondPosition, hasMarieSecond, err := nightSet.ResolveLocation("town.marie1")
	if err != nil {
		stage.Close()
		return fmt.Errorf("resolve Town Marie idle position: %w", err)
	}
	actorPoses := map[string]string{"leroy": "stand", "dog": "stand", "help": "stand", "jones": "stand", "buick": "stand", "marie": "stand", "isao": "stand"}
	actorHeadings := map[string]int16{"leroy": 0, "dog": 32, "help": 0, "jones": 0, "buick": 0, "marie": 128, "isao": 64}
	actorTurnTargets := map[string]int16{"leroy": 0, "help": 0, "jones": 0, "buick": 0, "marie": 128, "isao": 64}
	actorTurnActive, helpTurnActive, jonesTurnActive := false, false, false
	buickVisible, marieVisible := gameClock == 3, gameClock == 3
	buickTurnActive, marieTurnActive := false, false
	buickStar, marieStar := "town.blood1", "town.jones2"
	helpVisible, helpPhase, helpAttention := false, int16(0), int32(0)
	jonesPosition, jonesVisible := jonesStartPosition, false
	var isaoPosition [3]int16
	isaoVisible, isaoBouncer, isaoDirGo := false, false, false
	const leroyTurnRate int16 = 7
	const leroyWalkRate int16 = 3
	const helpWalkRate int16 = 3
	const helpTurnRate int16 = 7
	const jonesWalkRate int16 = 3
	const jonesTurnRate int16 = 7
	const townCastWalkRate int16 = 3
	const townActorHotDistance = 384
	const (
		leroyInteractionIdle uint8 = iota
		leroyInteractionMoving
		leroyInteractionFacing
		leroyInteractionPuppetPending
		leroyInteractionPuppetSpeaking
		leroyInteractionPuppetChoices
		leroyInteractionReturning
	)
	const (
		helpInteractionIdle uint8 = iota
		helpInteractionMoving
		helpInteractionFacing
		helpInteractionPuppetPending
		helpInteractionPuppetSpeaking
		helpInteractionPuppetChoices
		helpInteractionPuppetDelay
		helpInteractionReturning
	)
	const (
		jonesInteractionIdle uint8 = iota
		jonesInteractionMoving
		jonesInteractionFacing
		jonesInteractionPuppetPending
		jonesInteractionPuppetSpeaking
		jonesInteractionPuppetChoices
	)
	const (
		marieInteractionIdle uint8 = iota
		marieInteractionMoving
		marieInteractionFacing
		marieInteractionPuppetPending
		marieInteractionPuppetSpeaking
		marieInteractionPuppetChoices
		marieInteractionInventory
		marieInteractionInventoryReturning
	)
	const (
		isaoInteractionIdle uint8 = iota
		isaoInteractionPuppetPending
		isaoInteractionPuppetSpeaking
		isaoInteractionPuppetChoices
		isaoInteractionPuppetDelay
		isaoInteractionInventory
		isaoInteractionInventoryReturning
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
	helpInteractionStage := helpInteractionIdle
	jonesInteractionStage := jonesInteractionIdle
	jonesPhase, laurelPhase := int16(0), int16(0)
	jonesActorValue := int32(0)
	isaoInteractionStage := isaoInteractionIdle
	isaoPhase, isaoActorValue, oonaActorValue := int16(0), int32(0), int32(0)
	var isaoPuppet *render.Puppet
	var isaoPuppetTable assets.PuppetSpeechTable
	var isaoDialogue *engine.PuppetDialogue
	var isaoConversationBase, isaoChoiceBase render.IndexedFrame
	var isaoChoiceGroups map[string][][]scripts.PuppetChoice
	var isaoActiveChoices []scripts.PuppetChoice
	var isaoCurrentCode string
	isaoChoiceOutline := -1
	isaoNextCode, isaoNextChoiceGroup := "", -1
	var isaoChoicePressActive bool
	var isaoChoicePressEvent int32
	isaoChoicePressIndex := -1
	var isaoDialogueSkip bool
	var isaoSpeechFinishesRun bool
	var isaoSecondRun bool
	var isaoDelayUntil uint32
	var isaoInventoryReturnPending bool
	var isaoInventoryReturnCode string
	var isaoGiftCounter int32
	helpActorPosition, helpReturnPosition := helpPosition, helpPosition
	var helpWalk *scripts.NativeActorWalkJob
	var jonesWalk *scripts.NativeActorWalkJob
	var buickWalk, marieWalk *scripts.NativeActorWalkJob
	var buickWalkTarget, marieWalkTarget string
	var jonesPuppet *render.Puppet
	var jonesPuppetTable assets.PuppetSpeechTable
	var jonesPuppetProgram scripts.Program
	var jonesDialogue *engine.PuppetDialogue
	var jonesConversationBase, jonesChoiceBase render.IndexedFrame
	var jonesChoiceGroups [][]scripts.PuppetChoice
	var jonesActiveChoices []scripts.PuppetChoice
	var jonesChoiceGroup int
	var jonesDialogueSkip bool
	var jonesChoicePressActive bool
	var jonesChoicePressEvent int32
	jonesChoicePressIndex, jonesChoiceOutline := -1, -1
	jonesNextChoiceGroup := -1
	var jonesSetPhaseOnFinish bool
	marieInteractionStage := marieInteractionIdle
	mariePhase, marieFlag1, marieFlag2, marieFlag3 := int16(0), false, false, false
	marieActorValue := int32(0)
	handItem, handFlag := "", int16(0)
	var marieGiftCounter int32
	var mariePuppet *render.Puppet
	var mariePuppetTable assets.PuppetSpeechTable
	var mariePuppetProgram, marieInventoryProgram scripts.Program
	var marieHandBevelChoices []scripts.PuppetChoice
	var marieDialogue *engine.PuppetDialogue
	var marieConversationBase, marieChoiceBase render.IndexedFrame
	var marieChoiceGroups [][]scripts.PuppetChoice
	var marieActiveChoices []scripts.PuppetChoice
	var marieInventoryProjected []render.ProjectedFlatProp
	var marieInventoryReturnPending bool
	var marieChoiceGroup int
	var marieDialogueSkip bool
	var marieChoicePressActive bool
	var marieChoicePressEvent int32
	marieChoicePressIndex, marieChoiceOutline := -1, -1
	marieNextChoiceGroup := -1
	var marieSetPhaseOnFinish bool
	var marieFinishNPC bool
	var helpPuppet *render.Puppet
	var helpPuppetTable assets.PuppetSpeechTable
	var helpPuppetProgram scripts.Program
	var helpDialogue *engine.PuppetDialogue
	var helpDialogueSkip bool
	var helpConversationBase, helpChoiceBase render.IndexedFrame
	var helpActiveChoices []scripts.PuppetChoice
	var helpPage string
	var helpChoicePressActive bool
	var helpChoicePressEvent int32
	helpChoicePressIndex, helpChoiceOutline := -1, -1
	var helpSpeechStages []scripts.HelpSpeechStage
	var helpSpeechStageIndex int
	var helpPendingResult scripts.HelpChoiceResult
	var helpDelayUntil uint32
	var helpDelayReady bool
	var helpActorValue int32
	var helpInitialPage bool
	boneWorldProp := render.WorldPropSprite{Name: "Bone", Set: "town", Position: bonePosition, Heading: 32, Scale: 1200, Archive: inventoryArchive, View: boneSmallView}
	boneOwner := "none"
	var boneInventoryFrame render.PuppetFrame
	var boneInInventory bool
	inventoryOwners := map[string]string{"gun": "stranger", "boots": "stranger", "bullets": "stranger", "badge": "stranger", "hankerchief": "limbo", "ring": "none", "bone": "none"}
	inventoryHidden := make(map[string]bool)
	inventoryPropNames := map[string]string{"gun": "Gun", "boots": "Boots", "bullets": "Bullets", "badge": "Badge", "hankerchief": "Hankerchief", "ring": "Ring", "bone": "Bone"}
	inventoryHandNames := map[string]string{"gun": "gun", "boots": "boots", "bullets": "bullets", "badge": "badge", "hankerchief": "hankerchief", "ring": "ring", "bone": "Bone"}
	inventoryAnchors := map[string]image.Point{"gun": image.Pt(94, 213), "boots": image.Pt(249, 304), "badge": image.Pt(271, 151), "ring": image.Pt(185, 252), "bone": image.Pt(416, 191)}
	inventoryPropOrder := []string{"gun", "boots", "badge", "bullets", "hankerchief", "ring", "bone"}
	var boneDragging bool
	var boneDragLast image.Point
	var dog2PhaseAfterHelp bool
	defer func() {
		if isaoDialogue != nil {
			_ = isaoDialogue.Close()
		}
		if isaoPuppet != nil {
			_ = isaoPuppet.Close()
		}
		if marieDialogue != nil {
			_ = marieDialogue.Close()
		}
		if mariePuppet != nil {
			_ = mariePuppet.Close()
		}
		if jonesDialogue != nil {
			_ = jonesDialogue.Close()
		}
		if jonesPuppet != nil {
			_ = jonesPuppet.Close()
		}
		if leroyDialogue != nil {
			_ = leroyDialogue.Close()
		}
		if leroyPuppet != nil {
			_ = leroyPuppet.Close()
		}
		if helpDialogue != nil {
			_ = helpDialogue.Close()
		}
		if helpPuppet != nil {
			_ = helpPuppet.Close()
		}
	}()
	compositeWorld := func(background render.IndexedFrame, point [3]int16, actors []render.WorldActorSprite) (render.IndexedFrame, []render.ProjectedWorldActor, error) {
		return render.CompositeWorldActorsAndProps(background, point, activeSetName, actors, []render.WorldPropSprite{boneWorldProp})
	}
	loadWorldActors := func(point [3]int16) ([]render.WorldActorSprite, error) {
		actors := make([]render.WorldActorSprite, 0, 4)
		if activeSetName == "sallower" {
			if isaoVisible {
				for _, actor := range gangCast.Actors {
					if !strings.EqualFold(actor.Name, "Isao") {
						continue
					}
					actor.Position, actor.Located = isaoPosition, true
					sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["isao"], 0, 4200, render.NativeActorViewAngle(isaoPosition, point, actorHeadings["isao"]), 32)
					if err != nil {
						return nil, fmt.Errorf("load Sallowers actor Isao: %w", err)
					}
					actors = append(actors, sprite)
					break
				}
			}
			return actors, nil
		}
		if activeSetName != "town" {
			return actors, nil
		}
		for _, actor := range gangCast.Actors {
			if strings.EqualFold(actor.Name, "leroy") {
				if !hasLeroy {
					return nil, fmt.Errorf("NITE.SET has no town.leroy1 coordinate")
				}
				actor.Position, actor.Located = leroyPosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["leroy"], 0, 1100, render.NativeActorViewAngle(leroyPosition, point, actorHeadings["leroy"]), 32)
				if err != nil {
					return nil, fmt.Errorf("load G15 actor %s: %w", actor.Name, err)
				}
				actors = append(actors, sprite)
			} else if helpVisible && strings.EqualFold(actor.Name, "help") {
				if !hasHelp {
					return nil, fmt.Errorf("NITE.SET has no town.help coordinate")
				}
				actor.Position, actor.Located = helpActorPosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["help"], 0, 1450, render.NativeActorViewAngle(helpActorPosition, point, actorHeadings["help"]), 32)
				if err != nil {
					return nil, fmt.Errorf("load G15 actor %s: %w", actor.Name, err)
				}
				actors = append(actors, sprite)
			} else if jonesVisible && strings.EqualFold(actor.Name, "Jones") {
				actor.Position, actor.Located = jonesPosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["jones"], 0, 1450, render.NativeActorViewAngle(jonesPosition, point, actorHeadings["jones"]), 32)
				if err != nil {
					return nil, fmt.Errorf("load G15 actor %s: %w", actor.Name, err)
				}
				actors = append(actors, sprite)
			} else if buickVisible && hasBuick && strings.EqualFold(actor.Name, "Buick") {
				actor.Position, actor.Located = buickPosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["buick"], 0, 1450, render.NativeActorViewAngle(buickPosition, point, actorHeadings["buick"]), 32)
				if err != nil {
					return nil, fmt.Errorf("load Town actor %s: %w", actor.Name, err)
				}
				actors = append(actors, sprite)
			} else if marieVisible && hasMarie && strings.EqualFold(actor.Name, "Marie") {
				actor.Position, actor.Located = mariePosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["marie"], 0, 1450, render.NativeActorViewAngle(mariePosition, point, actorHeadings["marie"]), 32)
				if err != nil {
					return nil, fmt.Errorf("load Town actor %s: %w", actor.Name, err)
				}
				actors = append(actors, sprite)
			}
		}
		for _, actor := range townActors {
			if !dogVisibleState || gameDay != 1 || !strings.EqualFold(actor.Name, "dog") {
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
	worldBackground, projectedActors, err := compositeWorld(backgroundFrame, worldPoint, worldActors)
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
		frame, err = render.CompositePuppetFrame(frame, avatarFrames[degree], image.Pt(456, 328))
		if err != nil {
			return render.IndexedFrame{}, err
		}
		if !boneDragging {
			item := strings.ToLower(handItem)
			if inventoryOwners[item] == "stranger" && !inventoryHidden[item] {
				if itemFrame, found := inventoryLargeFrames[item]; found {
					return render.CompositePuppetFrame(frame, itemFrame, image.Pt(316, 320))
				}
			}
		}
		return frame, nil
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
	movieRestorePalette := render.BlackPaletteRaw()
	playback, err := render.NewMoviePlayback(movie, blackFrame, movieRestorePalette)
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
		worldBackground, projected, err := compositeWorld(backgroundFrame, worldPoint, actors)
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
	setWorldView := func(sceneName string, direction int16) (render.IndexedFrame, error) {
		nextView, found := activeSet.FindView(sceneName)
		if !found {
			return render.IndexedFrame{}, fmt.Errorf("%s has no view %q", activeSetName, sceneName)
		}
		nextPoint := [3]int16{int16(nextView.DirectionID), int16(nextView.SceneID), direction}
		frameResource, found, err := activeSet.BackgroundResourceForDirection(nextView, direction)
		if err != nil || !found {
			if err != nil {
				return render.IndexedFrame{}, fmt.Errorf("resolve %s background: %w", sceneName, err)
			}
			return render.IndexedFrame{}, fmt.Errorf("%s has no background for direction %d", sceneName, direction)
		}
		backgroundData, err := activeSet.Resource(frameResource)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("read %s background resource %d: %w", sceneName, frameResource, err)
		}
		backgroundPixels, decodeErr := render.DecodeMoviePixels(backgroundData, nil)
		if len(backgroundPixels.Pixels) == 0 {
			return render.IndexedFrame{}, fmt.Errorf("decode %s background resource %d: %w", sceneName, frameResource, decodeErr)
		}
		if decodeErr != nil && *debug {
			log.Printf("scene=%s resource=%d partial-frame: %v", sceneName, frameResource, decodeErr)
		}
		nextBackground, err := render.StageFrame(&assets.Stage{Width: uint16(backgroundPixels.Width), Height: uint16(backgroundPixels.Height), PaletteRaw: activeSet.Palette()}, backgroundPixels.Pixels)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("render %s background: %w", sceneName, err)
		}
		nextActors, err := loadWorldActors(nextPoint)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		nextWorldBackground, nextProjectedActors, err := compositeWorld(nextBackground, nextPoint, nextActors)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("render %s actors: %w", sceneName, err)
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("render mainpanel over %s: %w", sceneName, err)
		}
		nextFrame, err := composeMainPanel(nextWorldBackground, panel)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("compose %s scene: %w", sceneName, err)
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
		return nextFrame, nil
	}
	switchSpecialSet := func(setName, sceneName, directionName string) (render.IndexedFrame, error) {
		setName = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(setName, "DATA/")), ".set"))
		semanticName := setName
		var nextSet *assets.Set
		nextOwned := false
		if setName == "town" || setName == "nite" {
			semanticName = "town"
			if gameClock == 3 || setName == "nite" {
				nextSet = nightSet
			} else {
				nextSet, err = workspace.OpenSet("DATA/TOWN.SET")
				nextOwned = err == nil
			}
		} else {
			nextSet, err = workspace.OpenSet("DATA/" + strings.ToUpper(setName) + ".SET")
			nextOwned = err == nil
		}
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("open special SET %s: %w", setName, err)
		}
		closeNext := func(err error) (render.IndexedFrame, error) {
			if nextOwned {
				_ = nextSet.Close()
			}
			return render.IndexedFrame{}, err
		}
		startPoint, err := nextSet.StartPoint()
		if err != nil {
			return closeNext(fmt.Errorf("read %s start point: %w", setName, err))
		}
		nextView, found := assets.SetView{}, false
		if sceneName != "" {
			nextView, found = nextSet.FindView(sceneName)
		} else {
			for _, candidate := range nextSet.Views() {
				if candidate.DirectionID == uint16(startPoint[0]) && candidate.SceneID == uint16(startPoint[1]) {
					nextView, found = candidate, true
					break
				}
			}
		}
		if !found {
			return closeNext(fmt.Errorf("%s has no start view for scene %q", setName, sceneName))
		}
		direction := startPoint[2]
		if directionName != "" {
			switch strings.ToLower(directionName) {
			case "north":
				direction = assets.SetDirectionNorth
			case "south":
				direction = assets.SetDirectionSouth
			case "east":
				direction = assets.SetDirectionEast
			case "west":
				direction = assets.SetDirectionWest
			default:
				return closeNext(fmt.Errorf("unknown %s direction %q", setName, directionName))
			}
		}
		nextPoint := [3]int16{int16(nextView.DirectionID), int16(nextView.SceneID), direction}
		resource, found, err := nextSet.BackgroundResourceForDirection(nextView, direction)
		if err != nil || !found {
			if err != nil {
				return closeNext(fmt.Errorf("resolve %s/%s background: %w", setName, nextView.Name[1:], err))
			}
			return closeNext(fmt.Errorf("%s/%s has no background for direction %d", setName, nextView.Name[1:], direction))
		}
		data, err := nextSet.Resource(resource)
		if err != nil {
			return closeNext(fmt.Errorf("read %s background resource %d: %w", setName, resource, err))
		}
		pixels, decodeErr := render.DecodeMoviePixels(data, nil)
		if len(pixels.Pixels) == 0 {
			return closeNext(fmt.Errorf("decode %s background resource %d: %w", setName, resource, decodeErr))
		}
		if decodeErr != nil && *debug {
			log.Printf("set=%s resource=%d partial-frame: %v", setName, resource, decodeErr)
		}
		background, err := render.StageFrame(&assets.Stage{Width: uint16(pixels.Width), Height: uint16(pixels.Height), PaletteRaw: nextSet.Palette()}, pixels.Pixels)
		if err != nil {
			return closeNext(fmt.Errorf("render %s background: %w", setName, err))
		}
		previousSet, previousName, previousOwned := activeSet, activeSetName, activeSetOwned
		if previousName == "town" && semanticName != "town" {
			townReturnScene = string(view.Name[1:])
			nativeLoops.Stop(1, "scene g14")
			for _, owner := range []string{"leroy", "dog", "help", "jones", "buick", "marie", "isao"} {
				nativeLoops.Stop(2, owner)
			}
			leroyWalk, helpWalk, jonesWalk = nil, nil, nil
			buickWalk, marieWalk = nil, nil
			actorPoses["leroy"], actorPoses["help"], actorPoses["jones"] = "stand", "stand", "stand"
			actorTurnActive, helpTurnActive, jonesTurnActive = false, false, false
			buickTurnActive, marieTurnActive = false, false
		}
		activeSet, activeSetName, activeSetOwned = nextSet, semanticName, nextOwned
		view, worldPoint, backgroundFrame = nextView, nextPoint, background
		doorOwner = ""
		if semanticName == "sallower" {
			position, found, err := nextSet.ResolveLocation("sallower.isao")
			if err != nil || !found {
				return render.IndexedFrame{}, fmt.Errorf("resolve Sallowers Isao position: point=%v found=%t err=%v", position, found, err)
			}
			isaoPosition, isaoVisible, actorPoses["isao"], actorHeadings["isao"] = position, true, "stand", 64
			isaoBouncer, isaoDirGo, jonesTurnActive = false, false, false
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "isao", Callback: "isaoidle", Remaining: 2}); status != 0 {
				return render.IndexedFrame{}, fmt.Errorf("register Isao idle loop returned status %#x", status)
			}
		} else {
			isaoVisible = false
			nativeLoops.Stop(2, "isao")
		}
		nextActors, err := loadWorldActors(nextPoint)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		worldBackground, nextProjected, err := compositeWorld(background, nextPoint, nextActors)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("render %s actors: %w", setName, err)
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("render mainpanel over %s: %w", setName, err)
		}
		nextFrame, err := composeMainPanel(worldBackground, panel)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("compose %s scene: %w", setName, err)
		}
		worldActors, projectedActors, stageFrame, currentFrame = nextActors, nextProjected, nextFrame, nextFrame
		if semanticName == "town" && previousName != "town" {
			if gameClock == 3 {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "leroy", Callback: "leroyidle", Remaining: 20}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Leroy idle loop after town return returned status %#x", status)
				}
			}
			if buickVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "buick", Callback: "buickidle", Remaining: 21}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Buick idle loop after town return returned status %#x", status)
				}
			}
			if marieVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Marie idle loop after town return returned status %#x", status)
				}
			}
			if gameDay == 1 && dogVisibleState {
				step, found := scripts.DogIdleStep("doright", &nativeRandom)
				if found {
					actorPoses["dog"] = step.Pose
					if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "dog", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
						return render.IndexedFrame{}, fmt.Errorf("register Dog idle loop after town return returned status %#x", status)
					}
				}
			}
			if helpVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "help", Callback: "helpidle", Remaining: 19}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Help idle loop after town return returned status %#x", status)
				}
			}
			if jonesVisible && jonesWalk == nil {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Jones idle loop after town return returned status %#x", status)
				}
			}
			if currentThemeName == "nightwind3" {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 1, Owner: "scene g14", Callback: "nightfxs", Remaining: 2}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register NITE nightfxs loop after town return returned status %#x", status)
				}
			}
		}
		if previousOwned && previousSet != nextSet {
			if err := previousSet.Close(); err != nil {
				return render.IndexedFrame{}, fmt.Errorf("close previous active SET: %w", err)
			}
		}
		if semanticName == "town" {
			townReturnScene = ""
		}
		if *debug {
			log.Printf("set=%s view=%s direction=%d resource=%d size=%dx%d", setName, view.Name[1:], direction, resource, background.Width, background.Height)
			for _, actor := range projectedActors {
				log.Printf("world-actor=%s depth=%d bounds=%d,%d,%d,%d", actor.Name, actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
			}
		}
		return nextFrame, nil
	}
	setupHelpActor := func() error {
		if !hasHelp {
			return fmt.Errorf("GANG.CST Help setup has no town.help coordinate")
		}
		helpActorPosition = helpPosition
		if !helpVisible {
			actorHeadings["help"] = 0
		}
		helpVisible, actorPoses["help"] = true, "stand"
		nativeLoops.Stop(2, "help")
		camera := render.NativeActorCameraPosition(worldPoint)
		player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		dogVisible := false
		for _, actor := range worldActors {
			if strings.EqualFold(actor.Name, "dog") && actor.Visible {
				dogVisible = true
				break
			}
		}
		step, found := scripts.HelpIdleStep("helpidle", scripts.NativeActorDistance2D(helpActorPosition, player) < 384, dogVisible, gameDay, helpPhase)
		if !found {
			return fmt.Errorf("unknown Help idle callback")
		}
		actorPoses["help"] = step.Pose
		if step.ClearAttention {
			helpAttention = 0
		}
		if step.Attention != 0 {
			helpAttention = step.Attention
		}
		if step.TurnToCamera {
			actorTurnTargets["help"] = render.NativeActorHeadingToPoint(helpPosition, player)
			helpTurnActive = actorHeadings["help"] != actorTurnTargets["help"]
		}
		if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "help", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
			return fmt.Errorf("register Help idle loop returned status %#x", status)
		}
		if *debug {
			log.Printf("actor=help setup=dog point=%v pose=%s callback=%s ticks=%d attention=%d", helpActorPosition, step.Pose, step.Callback, step.Remaining, helpAttention)
		}
		return nil
	}
	resumeHelpIdle := func() error {
		camera := render.NativeActorCameraPosition(worldPoint)
		player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		dogVisible := false
		for _, actor := range worldActors {
			if strings.EqualFold(actor.Name, "dog") && actor.Visible {
				dogVisible = true
				break
			}
		}
		step, found := scripts.HelpIdleStep("helpidle", scripts.NativeActorDistance2D(helpActorPosition, player) < 384, dogVisible, gameDay, helpPhase)
		if !found {
			return fmt.Errorf("unknown Help idle callback")
		}
		actorPoses["help"], helpTurnActive = step.Pose, false
		if step.ClearAttention {
			helpAttention = 0
		}
		if step.Attention != 0 {
			helpAttention = step.Attention
		}
		if step.TurnToCamera {
			actorTurnTargets["help"] = render.NativeActorHeadingToPoint(helpActorPosition, player)
			helpTurnActive = actorHeadings["help"] != actorTurnTargets["help"]
		}
		nativeLoops.Stop(2, "help")
		if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "help", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
			return fmt.Errorf("register Help idle loop returned status %#x", status)
		}
		if *debug {
			log.Printf("actor=help idle=return point=%v pose=%s callback=%s ticks=%d heading=%d target=%d attention=%d", helpActorPosition, step.Pose, step.Callback, step.Remaining, actorHeadings["help"], actorTurnTargets["help"], helpAttention)
		}
		return nil
	}
	beginHelpPuppetTalk := func() (bool, error) {
		if !helpVisible || gameDay == 5 || helpInteractionStage != helpInteractionIdle {
			return false, nil
		}
		camera := render.NativeActorCameraPosition(worldPoint)
		playerPosition := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		distance := scripts.NativeActorDistance2D(helpActorPosition, playerPosition)
		if distance >= 384 {
			return false, nil
		}
		if !scripts.NativeWalktopuppetAxisAligned(helpActorPosition, playerPosition) {
			if *debug {
				log.Printf("actor=help walktopuppet=blocked-axis-alignment actor=%v player=%v", helpActorPosition, playerPosition)
			}
			return false, nil
		}
		destination := [3]int16{playerPosition[0], playerPosition[1], 0}
		routeHeading := render.NativeActorHeadingToPoint(helpActorPosition, destination)
		helpReturnPosition = helpActorPosition
		walk := scripts.NewNativeActorWalkJob(helpActorPosition, destination, routeHeading, helpWalkRate)
		helpWalk, helpInteractionStage = &walk, helpInteractionMoving
		helpTurnActive, actorPoses["help"] = false, "stand"
		nativeLoops.Stop(2, "help")
		if *debug {
			log.Printf("actor=help walktopuppet distance=%d destination=%v heading=%d puppet=HELP1.PUP", distance, destination, routeHeading)
		}
		return true, nil
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
		dialogueBackground, _, err := compositeWorld(backgroundFrame, worldPoint, dialogueActors)
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
	openHelpPuppet := func() error {
		if helpPuppet != nil {
			return nil
		}
		puppet, err := render.OpenPuppet(workspace, "PUPPETS/HELP1.PUP")
		if err != nil {
			return err
		}
		cache, err := workspace.OpenResourceCache("PUPPETS/HELP1.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		defer cache.Close()
		lease, err := cache.Acquire(33)
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
		table, err := workspace.OpenPuppetSpeechTable("PUPPETS/HELP1.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		helpPuppet, helpPuppetTable, helpPuppetProgram = puppet, table, program
		return nil
	}
	drawHelpChoices := func(outline int) error {
		labels := make([]string, len(helpActiveChoices))
		for index, choice := range helpActiveChoices {
			labels[index] = choice.Text
		}
		frame, err := helpPuppet.ChoiceFrame(helpChoiceBase, helpPuppetTable.PanelResource, labels)
		if err != nil {
			return fmt.Errorf("render Help choice panel: %w", err)
		}
		if outline >= 0 {
			frame, err = render.DrawNativePuppetChoiceBevel(frame, outline)
			if err != nil {
				return fmt.Errorf("render Help choice bevel: %w", err)
			}
		}
		currentFrame, stageFrame = frame, frame
		helpChoiceOutline = outline
		return nil
	}
	showHelpChoices := func(page string) error {
		choices, err := scripts.PuppetBevelChoices(helpPuppetProgram, page)
		if err != nil {
			return err
		}
		if len(choices) == 0 {
			return fmt.Errorf("HELP1.PUP %s reached its event wait with no choices", page)
		}
		helpPage, helpActiveChoices = page, choices
		helpChoiceBase, helpChoicePressActive = currentFrame, false
		helpChoicePressIndex, helpChoiceOutline = -1, -1
		if err := drawHelpChoices(-1); err != nil {
			return err
		}
		helpInteractionStage = helpInteractionPuppetChoices
		return nil
	}
	startHelpSpeechStage := func() error {
		stage := helpSpeechStages[helpSpeechStageIndex]
		if stage.DelayBefore > 0 && !helpDelayReady {
			helpDelayUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + stage.DelayBefore
			helpInteractionStage = helpInteractionPuppetDelay
			return nil
		}
		helpDelayReady = false
		if helpDialogue != nil {
			if err := helpDialogue.Close(); err != nil {
				return err
			}
		}
		dialogue, err := engine.NewPuppetDialogue(helpPuppet, helpPuppetTable.Entries, stage.Lines, audioContext)
		if err != nil {
			return err
		}
		helpDialogue = dialogue
		frame, err := helpDialogue.Start(helpConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			return err
		}
		currentFrame, stageFrame = frame, frame
		helpInteractionStage = helpInteractionPuppetSpeaking
		if *debug {
			log.Printf("puppet=help page=%s lines=%d", helpPage, len(stage.Lines))
		}
		return nil
	}
	startHelpPage := func(page string) error {
		lines, err := scripts.PuppetSpeechCalls(helpPuppetProgram, page, scripts.LookupOpcode("puppetclear"))
		if err != nil {
			return err
		}
		helpPage, helpInitialPage = page, true
		if len(lines) == 0 {
			helpInitialPage = false
			return showHelpChoices(page)
		}
		helpSpeechStages, helpSpeechStageIndex = []scripts.HelpSpeechStage{{Lines: lines}}, 0
		return startHelpSpeechStage()
	}
	startHelpPuppet := func() error {
		if err := openHelpPuppet(); err != nil {
			return fmt.Errorf("open Help dialogue: %w", err)
		}
		dialogueActors := make([]render.WorldActorSprite, 0, len(worldActors))
		for _, actor := range worldActors {
			if !strings.EqualFold(actor.Name, "Help") {
				dialogueActors = append(dialogueActors, actor)
			}
		}
		dialogueBackground, _, err := compositeWorld(backgroundFrame, worldPoint, dialogueActors)
		if err != nil {
			return fmt.Errorf("hide Help world sprite for dialogue: %w", err)
		}
		dialoguePanel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return fmt.Errorf("render Help dialogue panel: %w", err)
		}
		helpConversationBase, err = composeMainPanel(dialogueBackground, dialoguePanel)
		if err != nil {
			return fmt.Errorf("compose Help dialogue background: %w", err)
		}
		palette, err := helpPuppet.Palette()
		if err != nil {
			return fmt.Errorf("load Help PUP CLUT: %w", err)
		}
		helpConversationBase.Palette = palette
		helpPendingResult = scripts.HelpChoiceResult{}
		helpDelayReady, helpInitialPage = false, false
		return startHelpPage(scripts.HelpPuppetEntryPage(helpPhase, dogVisibleState))
	}
	completeHelpReturn := func() error {
		actorPoses["help"] = "stand"
		helpActorValue++
		helpInteractionStage = helpInteractionIdle
		if dog2PhaseAfterHelp {
			gamePhase, dog2PhaseAfterHelp = 2, false
			if *debug {
				log.Printf("actor=dog offerobject=complete phase=%d", gamePhase)
			}
		}
		return resumeHelpIdle()
	}
	returnHelpToStar := func() error {
		if helpActorPosition == helpReturnPosition {
			if err := completeHelpReturn(); err != nil {
				return err
			}
			return refreshWorldScene()
		}
		heading := render.NativeActorHeadingToPoint(helpActorPosition, helpReturnPosition)
		walk := scripts.NewNativeActorWalkJob(helpActorPosition, helpReturnPosition, heading, helpWalkRate)
		helpWalk, helpInteractionStage = &walk, helpInteractionReturning
		return nil
	}
	openJonesPuppet := func() error {
		if jonesPuppet != nil {
			return nil
		}
		puppet, err := render.OpenPuppet(workspace, "PUPPETS/JONES.PUP")
		if err != nil {
			return err
		}
		cache, err := workspace.OpenResourceCache("PUPPETS/JONES.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		defer cache.Close()
		lease, err := cache.Acquire(74)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = puppet.Close()
			return err
		}
		program, err := scripts.ParseProgram(data)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		groups, err := scripts.PuppetBevelChoiceGroups(program, "threenite")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		table, err := workspace.OpenPuppetSpeechTable("PUPPETS/JONES.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		jonesPuppet, jonesPuppetTable, jonesPuppetProgram, jonesChoiceGroups = puppet, table, program, groups
		return nil
	}
	openIsaoPuppet := func() error {
		if isaoPuppet != nil {
			return nil
		}
		puppet, err := render.OpenPuppet(workspace, "PUPPETS/ISAO.PUP")
		if err != nil {
			return err
		}
		table, err := workspace.OpenPuppetSpeechTable("PUPPETS/ISAO.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		cache, err := workspace.OpenResourceCache("PUPPETS/ISAO.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		lease, err := cache.Acquire(22)
		if err != nil {
			_ = cache.Close()
			_ = puppet.Close()
			return err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if closeErr := cache.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = puppet.Close()
			return err
		}
		program, err := scripts.ParseProgram(data)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		if len(marieInventoryProgram.Records) == 0 {
			inventoryData, err := inventoryArchive.Resource(1)
			if err != nil {
				_ = puppet.Close()
				return err
			}
			inventoryProgram, err := scripts.ParseProgram(inventoryData)
			if err != nil {
				_ = puppet.Close()
				return err
			}
			handChoices, err := scripts.PuppetBevelChoices(inventoryProgram, "addhandbevel")
			if err != nil {
				_ = puppet.Close()
				return err
			}
			marieInventoryProgram, marieHandBevelChoices = inventoryProgram, handChoices
		}
		groups := make(map[string][][]scripts.PuppetChoice, 5)
		for _, code := range []string{"runyoself", "whoareyou", "byenow", "brushoff", "ring"} {
			groups[code], err = scripts.PuppetBevelChoiceGroups(program, code)
			if err != nil {
				_ = puppet.Close()
				return err
			}
		}
		isaoPuppet, isaoPuppetTable, isaoChoiceGroups = puppet, table, groups
		return nil
	}
	var buildIsaoConversationBase func() error
	var resumeIsaoInventory func() error
	var openIsaoInventory func() error
	var drawIsaoChoices func(int) error
	var showIsaoChoices func(string, int) error
	var startIsaoSpeech func([]string, string, int, bool) error
	var startIsaoConversation func() error
	var finishIsaoSpeech func() error
	var finishIsaoPuppetRun func() error
	var startIsaoEventResponse func(int32) error
	drawIsaoChoices = func(outline int) error {
		labels := make([]string, len(isaoActiveChoices))
		for index, choice := range isaoActiveChoices {
			labels[index] = choice.Text
		}
		frame, err := isaoPuppet.ChoiceFrame(isaoChoiceBase, isaoPuppetTable.PanelResource, labels)
		if err != nil {
			return fmt.Errorf("render Isao choice panel: %w", err)
		}
		if outline >= 0 {
			frame, err = render.DrawNativePuppetChoiceBevel(frame, outline)
			if err != nil {
				return fmt.Errorf("render Isao choice bevel: %w", err)
			}
		}
		currentFrame, stageFrame, isaoChoiceOutline = frame, frame, outline
		return nil
	}
	finishIsaoSpeech = func() error {
		if isaoDialogue != nil {
			if err := isaoDialogue.Close(); err != nil {
				return err
			}
			isaoDialogue = nil
		}
		if isaoNextCode != "" {
			code, group := isaoNextCode, isaoNextChoiceGroup
			isaoNextCode, isaoNextChoiceGroup = "", -1
			return showIsaoChoices(code, group)
		}
		if isaoSpeechFinishesRun {
			isaoSpeechFinishesRun = false
			return finishIsaoPuppetRun()
		}
		return nil
	}
	showIsaoChoices = func(code string, group int) error {
		groups, found := isaoChoiceGroups[code]
		if !found || group < 0 || group >= len(groups) {
			return fmt.Errorf("Isao %s choice group %d is unavailable", code, group)
		}
		choices := append([]scripts.PuppetChoice(nil), groups[group]...)
		if code == "ring" && len(choices) == 2 {
			if inventoryOwners["ring"] == "isao" {
				choices = choices[:1]
			} else {
				choices = choices[1:]
			}
		}
		if code == "runyoself" && group == 0 && (handFlag == 1 || handItem != "" && !inventoryHidden[strings.ToLower(handItem)]) {
			var handChoice scripts.PuppetChoice
			var hasHandChoice bool
			var err error
			if handFlag == 1 && len(marieHandBevelChoices) > 0 {
				handChoice, hasHandChoice = marieHandBevelChoices[0], true
			} else if handItem != "" {
				handChoice, hasHandChoice, err = scripts.PuppetBevelChoiceInCase(marieInventoryProgram, "addhandbevel", "handitem", handItem)
			}
			if err != nil {
				return err
			}
			if hasHandChoice {
				choices = append(choices, handChoice)
			}
		}
		if len(choices) == 0 {
			return fmt.Errorf("Isao %s choice group %d is empty", code, group)
		}
		isaoCurrentCode, isaoActiveChoices = code, choices
		isaoChoiceBase, isaoChoicePressActive = currentFrame, false
		isaoChoicePressIndex, isaoChoiceOutline = -1, -1
		if err := drawIsaoChoices(-1); err != nil {
			return err
		}
		isaoInteractionStage = isaoInteractionPuppetChoices
		return nil
	}
	startIsaoSpeech = func(calls []string, nextCode string, nextGroup int, finishRun bool) error {
		if isaoDialogue != nil {
			if err := isaoDialogue.Close(); err != nil {
				return err
			}
		}
		dialogue, err := engine.NewPuppetDialogue(isaoPuppet, isaoPuppetTable.Entries, calls, audioContext)
		if err != nil {
			return err
		}
		frame, err := dialogue.Start(isaoConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			_ = dialogue.Close()
			return err
		}
		isaoDialogue, isaoNextCode, isaoNextChoiceGroup, isaoSpeechFinishesRun = dialogue, nextCode, nextGroup, finishRun
		isaoInteractionStage = isaoInteractionPuppetSpeaking
		currentFrame, stageFrame = frame, frame
		if *debug {
			log.Printf("puppet=isao speech-lines=%d next-code=%s next-choice-group=%d finish-run=%t", len(calls), nextCode, nextGroup, finishRun)
		}
		return nil
	}
	buildIsaoConversationBase = func() error {
		if isaoPuppet == nil {
			return fmt.Errorf("Isao PUP is unavailable")
		}
		dialogueBackground, _, err := render.CompositeWorldActors(backgroundFrame, worldPoint, worldActors)
		if err != nil {
			return fmt.Errorf("render Isao dialogue actors: %w", err)
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return fmt.Errorf("render Isao dialogue panel: %w", err)
		}
		base, err := composeMainPanel(dialogueBackground, panel)
		if err != nil {
			return fmt.Errorf("compose Isao dialogue background: %w", err)
		}
		base.Palette, err = isaoPuppet.Palette()
		if err != nil {
			return fmt.Errorf("load Isao PUP CLUT: %w", err)
		}
		idleFrame, err := isaoPuppet.Frame(0, 0)
		if err != nil {
			return fmt.Errorf("load Isao initial PUP frame: %w", err)
		}
		isaoConversationBase, err = render.CompositePuppetFrame(base, idleFrame, idleFrame.Origin)
		if err != nil {
			return fmt.Errorf("compose Isao initial PUP frame: %w", err)
		}
		return nil
	}
	resumeIsaoInventory = func() error {
		code := isaoInventoryReturnCode
		isaoInventoryReturnCode = ""
		if handItem != "" {
			gift, found := scripts.IsaoGift(isaoGiftCounter)
			if !found {
				return fmt.Errorf("Isao gift script has no counter case %d for %q", isaoGiftCounter, handItem)
			}
			giftedItem := handItem
			inventoryHidden[strings.ToLower(handItem)], isaoGiftCounter = true, gift.Counter
			if err := buildIsaoConversationBase(); err != nil {
				return err
			}
			if *debug {
				log.Printf("puppet=isao gift=%s lines=%d counter=%d", giftedItem, len(gift.Speech), isaoGiftCounter)
			}
			return startIsaoSpeech(gift.Speech, "byenow", 0, false)
		}
		if err := buildIsaoConversationBase(); err != nil {
			return err
		}
		currentFrame, stageFrame = isaoConversationBase, isaoConversationBase
		isaoInteractionStage = isaoInteractionPuppetChoices
		return showIsaoChoices(code, 0)
	}
	finishIsaoPuppetRun = func() error {
		if isaoDialogue != nil {
			if err := isaoDialogue.Close(); err != nil {
				return err
			}
			isaoDialogue = nil
		}
		if !isaoSecondRun {
			isaoActorValue++
			isaoPhase, isaoSecondRun = 999, true
			isaoDelayUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + 60
			isaoInteractionStage = isaoInteractionPuppetDelay
			return nil
		}
		isaoActorValue++
		isaoSecondRun, isaoInteractionStage = false, isaoInteractionIdle
		actorPoses["isao"] = "stand"
		if isaoVisible && activeSetName == "sallower" {
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "isao", Callback: "isaoidle", Remaining: 2}); status != 0 {
				return fmt.Errorf("register Isao idle loop after dialogue returned status %#x", status)
			}
		}
		if *debug {
			log.Printf("actor=isao mousedown-complete actorvalue=%d isaophase=%d visible=%t", isaoActorValue, isaoPhase, isaoVisible)
		}
		return refreshWorldScene()
	}
	startIsaoEventResponse = func(event int32) error {
		state := scripts.IsaoState{Day: gameDay, Clock: gameClock, Phase: gamePhase, IsaoPhase: isaoPhase, OonaActorValue: oonaActorValue, RingOwner: inventoryOwners["ring"]}
		randomChoice := uint32(0)
		if isaoCurrentCode == "whoareyou" && event == 102 {
			randomChoice = nativeRandom.Inclusive(3)
		}
		step, err := scripts.IsaoPuppetResponse(isaoCurrentCode, event, state, randomChoice)
		if err != nil {
			return err
		}
		if step.SetIsaoPhaseValid {
			isaoPhase = step.SetIsaoPhase
		}
		if step.OpenInventory {
			if handItem == "" || handFlag == 1 {
				return openIsaoInventory()
			}
			gift, found := scripts.IsaoGift(isaoGiftCounter)
			if !found {
				return fmt.Errorf("Isao gift script has no counter case %d for %q", isaoGiftCounter, handItem)
			}
			giftedItem := handItem
			inventoryHidden[strings.ToLower(handItem)], isaoGiftCounter = true, gift.Counter
			if err := buildIsaoConversationBase(); err != nil {
				return err
			}
			if *debug {
				log.Printf("puppet=isao gift=%s lines=%d counter=%d", giftedItem, len(gift.Speech), isaoGiftCounter)
			}
			return startIsaoSpeech(gift.Speech, "byenow", 0, false)
		}
		if len(step.Speech) > 0 {
			return startIsaoSpeech(step.Speech, step.NextCode, step.NextGroup, step.Finish)
		}
		if step.NextCode != "" {
			return showIsaoChoices(step.NextCode, step.NextGroup)
		}
		if step.Finish {
			return finishIsaoPuppetRun()
		}
		return nil
	}
	startIsaoConversation = func() error {
		if err := openIsaoPuppet(); err != nil {
			return fmt.Errorf("open Isao dialogue: %w", err)
		}
		if err := buildIsaoConversationBase(); err != nil {
			return err
		}
		state := scripts.IsaoState{Day: gameDay, Clock: gameClock, Phase: gamePhase, IsaoPhase: isaoPhase, OonaActorValue: oonaActorValue, RingOwner: inventoryOwners["ring"]}
		step := scripts.IsaoEntry(state)
		if step.SetIsaoPhaseValid {
			isaoPhase = step.SetIsaoPhase
			isaoSecondRun = true
		}
		if len(step.Speech) > 0 {
			return startIsaoSpeech(step.Speech, "", -1, true)
		}
		if *debug {
			log.Printf("puppet=isao entry-code=%s day=%d clock=%d phase=%d isaophase=%d oonapvalue=%d", step.Code, gameDay, gameClock, gamePhase, isaoPhase, oonaActorValue)
		}
		return showIsaoChoices(step.Code, 0)
	}
	beginIsaoPuppetTalk := func() (bool, error) {
		if activeSetName != "sallower" || !isaoVisible || isaoInteractionStage != isaoInteractionIdle {
			return false, nil
		}
		camera := render.NativeActorCameraPosition(worldPoint)
		player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		distance := scripts.NativeActorDistance2D(isaoPosition, player)
		if distance >= townActorHotDistance {
			if *debug {
				log.Printf("actor=isao mousedown=ignored distance=%d hotdist=%d", distance, townActorHotDistance)
			}
			return false, nil
		}
		nativeLoops.Stop(2, "isao")
		isaoInteractionStage = isaoInteractionPuppetPending
		if *debug {
			log.Printf("actor=isao mousedown=accepted distance=%d point=%v", distance, worldPoint)
		}
		return true, nil
	}
	var drawJonesChoices func(int) error
	var showJonesChoices func(int) error
	var finishJonesDialogue func() error
	var startJonesSpeech func([]string, int, bool) error
	drawJonesChoices = func(outline int) error {
		labels := make([]string, len(jonesActiveChoices))
		for index, choice := range jonesActiveChoices {
			labels[index] = choice.Text
		}
		frame, err := jonesPuppet.ChoiceFrame(jonesChoiceBase, jonesPuppetTable.PanelResource, labels)
		if err != nil {
			return fmt.Errorf("render Jones choice panel: %w", err)
		}
		if outline >= 0 {
			frame, err = render.DrawNativePuppetChoiceBevel(frame, outline)
			if err != nil {
				return fmt.Errorf("render Jones choice bevel: %w", err)
			}
		}
		currentFrame, stageFrame = frame, frame
		jonesChoiceOutline = outline
		return nil
	}
	showJonesChoices = func(group int) error {
		if group < 0 || group >= len(jonesChoiceGroups) {
			return fmt.Errorf("Jones threenite choice group %d is outside %d groups", group, len(jonesChoiceGroups))
		}
		choices := jonesChoiceGroups[group]
		if group == 0 && len(choices) == 3 {
			if laurelPhase == 1 {
				choices = []scripts.PuppetChoice{choices[0], choices[2]}
			} else {
				choices = []scripts.PuppetChoice{choices[1], choices[2]}
			}
		}
		jonesActiveChoices = append(jonesActiveChoices[:0], choices...)
		if len(jonesActiveChoices) == 0 {
			return fmt.Errorf("Jones threenite group %d has no native choices", group)
		}
		jonesChoiceGroup = group
		jonesChoiceBase, jonesChoicePressActive = currentFrame, false
		jonesChoicePressIndex, jonesChoiceOutline = -1, -1
		if err := drawJonesChoices(-1); err != nil {
			return err
		}
		jonesInteractionStage = jonesInteractionPuppetChoices
		return nil
	}
	startJonesSpeech = func(calls []string, nextGroup int, setPhase bool) error {
		if jonesDialogue != nil {
			if err := jonesDialogue.Close(); err != nil {
				return err
			}
		}
		dialogue, err := engine.NewPuppetDialogue(jonesPuppet, jonesPuppetTable.Entries, calls, audioContext)
		if err != nil {
			return err
		}
		frame, err := dialogue.Start(jonesConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			_ = dialogue.Close()
			return err
		}
		jonesDialogue, jonesNextChoiceGroup, jonesSetPhaseOnFinish = dialogue, nextGroup, setPhase
		jonesInteractionStage = jonesInteractionPuppetSpeaking
		currentFrame, stageFrame = frame, frame
		if *debug {
			log.Printf("puppet=jones speech-lines=%d next-choice-group=%d phase-on-finish=%t", len(calls), nextGroup, setPhase)
		}
		return nil
	}
	finishJonesDialogue = func() error {
		if jonesSetPhaseOnFinish {
			jonesPhase, jonesSetPhaseOnFinish = 1, false
		}
		if jonesDialogue != nil {
			if err := jonesDialogue.Close(); err != nil {
				return err
			}
			jonesDialogue = nil
		}
		if jonesNextChoiceGroup >= 0 {
			group := jonesNextChoiceGroup
			jonesNextChoiceGroup = -1
			return showJonesChoices(group)
		}
		jonesActorValue++
		jonesInteractionStage = jonesInteractionIdle
		actorPoses["jones"] = "stand"
		if jonesVisible && activeSetName == "town" {
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
				return fmt.Errorf("register Jones idle loop after dialogue returned status %#x", status)
			}
		}
		if *debug {
			log.Printf("actor=jones mousedown-complete actorvalue=%d jonesphase=%d", jonesActorValue, jonesPhase)
		}
		return refreshWorldScene()
	}
	startJonesEventResponse := func(event int32) error {
		var nextGroup int
		setPhase := false
		switch jonesChoiceGroup {
		case 0:
			if event != 101 {
				return fmt.Errorf("Jones threenite group 0 returned event %d", event)
			}
			nextGroup = 1
		case 1:
			if event != 102 {
				return fmt.Errorf("Jones threenite group 1 returned event %d", event)
			}
			nextGroup = 2
		case 2:
			if event != 201 {
				return fmt.Errorf("Jones threenite group 2 returned event %d", event)
			}
			nextGroup, setPhase = -1, true
		default:
			return fmt.Errorf("Jones threenite has unsupported choice group %d", jonesChoiceGroup)
		}
		calls, err := scripts.PuppetEventSpeechCalls(jonesPuppetProgram, "threenite", event)
		if err != nil {
			return err
		}
		return startJonesSpeech(calls, nextGroup, setPhase)
	}
	startJonesConversation := func() error {
		if err := openJonesPuppet(); err != nil {
			return fmt.Errorf("open Jones dialogue: %w", err)
		}
		if gameClock != 3 {
			jonesInteractionStage = jonesInteractionIdle
			if *debug {
				log.Printf("puppet=jones blocked clock=%d jonesphase=%d", gameClock, jonesPhase)
			}
			if jonesVisible && activeSetName == "town" {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
					return fmt.Errorf("register Jones idle loop after unsupported dialogue returned status %#x", status)
				}
			}
			return nil
		}
		dialogueActors := make([]render.WorldActorSprite, 0, len(worldActors))
		for _, actor := range worldActors {
			if !strings.EqualFold(actor.Name, "jones") {
				dialogueActors = append(dialogueActors, actor)
			}
		}
		dialogueBackground, _, err := compositeWorld(backgroundFrame, worldPoint, dialogueActors)
		if err != nil {
			return fmt.Errorf("hide Jones world sprite for dialogue: %w", err)
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return fmt.Errorf("render Jones dialogue panel: %w", err)
		}
		jonesConversationBase, err = composeMainPanel(dialogueBackground, panel)
		if err != nil {
			return fmt.Errorf("compose Jones dialogue background: %w", err)
		}
		palette, err := jonesPuppet.Palette()
		if err != nil {
			return fmt.Errorf("load Jones PUP CLUT: %w", err)
		}
		jonesConversationBase.Palette = palette
		if jonesPhase == 0 {
			calls, err := scripts.PuppetSpeechCalls(jonesPuppetProgram, "threenite", scripts.LookupOpcode("puppetclear"))
			if err != nil {
				return err
			}
			return startJonesSpeech(calls, 0, false)
		}
		if jonesPhase == 1 && laurelPhase != 2 {
			return startJonesSpeech([]string{"jones.122", "jones.123", "jones.124"}, -1, false)
		}
		jonesInteractionStage = jonesInteractionIdle
		if *debug {
			log.Printf("puppet=jones blocked jonesphase=%d laurelphase=%d", jonesPhase, laurelPhase)
		}
		if jonesVisible && activeSetName == "town" {
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
				return fmt.Errorf("register Jones idle loop after unsupported state returned status %#x", status)
			}
		}
		return refreshWorldScene()
	}
	beginJonesPuppetTalk := func() (bool, error) {
		if activeSetName != "town" || !jonesVisible || jonesInteractionStage != jonesInteractionIdle || jonesWalk != nil {
			return false, nil
		}
		if gameClock != 3 || jonesPhase == 1 && laurelPhase == 2 {
			if *debug {
				log.Printf("puppet=jones unavailable clock=%d jonesphase=%d laurelphase=%d", gameClock, jonesPhase, laurelPhase)
			}
			return false, nil
		}
		camera := render.NativeActorCameraPosition(worldPoint)
		playerPosition := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		nativeLoops.Stop(2, "jones")
		jonesTurnActive = false
		if scripts.NativeActorDistance2D(jonesPosition, playerPosition) >= townActorHotDistance {
			destination := [3]int16{playerPosition[0], playerPosition[1], 0}
			routeHeading := render.NativeActorHeadingToPoint(jonesPosition, destination)
			walk := scripts.NewNativeActorWalkJob(jonesPosition, destination, routeHeading, jonesWalkRate)
			jonesWalk, jonesInteractionStage = &walk, jonesInteractionMoving
			if *debug {
				log.Printf("actor=jones walktopuppet destination=%v heading=%d", destination, routeHeading)
			}
			return true, nil
		}
		actorTurnTargets["jones"] = render.NativeActorHeadingToPoint(jonesPosition, playerPosition)
		jonesTurnActive = actorHeadings["jones"] != actorTurnTargets["jones"]
		jonesInteractionStage = jonesInteractionFacing
		if !jonesTurnActive {
			jonesInteractionStage = jonesInteractionPuppetPending
		}
		return true, nil
	}
	openMariePuppet := func() error {
		if mariePuppet != nil {
			return nil
		}
		puppet, err := render.OpenPuppet(workspace, "PUPPETS/MARIE.PUP")
		if err != nil {
			return err
		}
		cache, err := workspace.OpenResourceCache("PUPPETS/MARIE.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		lease, err := cache.Acquire(54)
		if err != nil {
			_ = cache.Close()
			_ = puppet.Close()
			return err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if closeErr := cache.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = puppet.Close()
			return err
		}
		program, err := scripts.ParseProgram(data)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		groups, err := scripts.PuppetBevelChoiceGroups(program, "twonite")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		table, err := workspace.OpenPuppetSpeechTable("PUPPETS/MARIE.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		inventoryCache, err := workspace.OpenResourceCache("DATA/INVEN.PRP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		inventoryLease, err := inventoryCache.Acquire(1)
		if err != nil {
			_ = inventoryCache.Close()
			_ = puppet.Close()
			return err
		}
		inventoryData, err := inventoryLease.Bytes()
		if closeErr := inventoryLease.Close(); err == nil {
			err = closeErr
		}
		if closeErr := inventoryCache.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = puppet.Close()
			return err
		}
		inventoryProgram, err := scripts.ParseProgram(inventoryData)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		handChoices, err := scripts.PuppetBevelChoices(inventoryProgram, "addhandbevel")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		mariePuppet, mariePuppetTable, mariePuppetProgram, marieInventoryProgram = puppet, table, program, inventoryProgram
		marieChoiceGroups, marieHandBevelChoices = groups, handChoices
		return nil
	}
	var drawMarieChoices func(int) error
	var showMarieChoices func(int) error
	var finishMarieDialogue func() error
	var startMarieSpeech func([]string, int, bool, bool) error
	drawMarieChoices = func(outline int) error {
		labels := make([]string, len(marieActiveChoices))
		for index, choice := range marieActiveChoices {
			labels[index] = choice.Text
		}
		frame, err := mariePuppet.ChoiceFrame(marieChoiceBase, mariePuppetTable.PanelResource, labels)
		if err != nil {
			return fmt.Errorf("render Marie choice panel: %w", err)
		}
		if outline >= 0 {
			frame, err = render.DrawNativePuppetChoiceBevel(frame, outline)
			if err != nil {
				return fmt.Errorf("render Marie choice bevel: %w", err)
			}
		}
		currentFrame, stageFrame = frame, frame
		marieChoiceOutline = outline
		return nil
	}
	showMarieChoices = func(group int) error {
		if group < 0 || group >= len(marieChoiceGroups) {
			return fmt.Errorf("Marie twonite choice group %d is outside %d groups", group, len(marieChoiceGroups))
		}
		marieActiveChoices = marieActiveChoices[:0]
		for _, choice := range marieChoiceGroups[group] {
			include := true
			if group == 2 {
				switch choice.EventID {
				case 203:
					include = !marieFlag1
				case 102:
					include = !marieFlag2
				case 103:
					include = !marieFlag3
				}
			}
			if include {
				marieActiveChoices = append(marieActiveChoices, choice)
			}
		}
		if group == 2 && len(marieActiveChoices) <= 3 {
			var handChoice scripts.PuppetChoice
			var found bool
			var err error
			if handFlag == 1 && len(marieHandBevelChoices) > 0 {
				handChoice, found = marieHandBevelChoices[0], true
			} else {
				handChoice, found, err = scripts.PuppetBevelChoiceInCase(marieInventoryProgram, "addhandbevel", "handitem", handItem)
			}
			if err != nil {
				return err
			}
			if found {
				marieActiveChoices = append(marieActiveChoices, handChoice)
			}
		}
		if len(marieActiveChoices) == 0 {
			return fmt.Errorf("Marie twonite group %d has no native choices", group)
		}
		marieChoiceGroup = group
		marieChoiceBase, marieChoicePressActive = currentFrame, false
		marieChoicePressIndex, marieChoiceOutline = -1, -1
		if err := drawMarieChoices(-1); err != nil {
			return err
		}
		marieInteractionStage = marieInteractionPuppetChoices
		return nil
	}
	renderMarieInventory := func() error {
		lease, err := stage.AcquireSceneFrameResource(2)
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
		pixels, decodeErr := render.DecodeMoviePixels(data, currentPixels.Pixels)
		if len(pixels.Pixels) == 0 {
			return fmt.Errorf("decode avatar inventory flat: %w", decodeErr)
		}
		if decodeErr != nil && *debug {
			log.Printf("scene=avatar partial-frame: %v", decodeErr)
		}
		base, err := render.StageFrame(stage, pixels.Pixels)
		if err != nil {
			return err
		}
		props := make([]render.FlatPropSprite, 0, len(inventoryPropOrder))
		for _, key := range inventoryPropOrder {
			if inventoryOwners[key] != "stranger" || inventoryHidden[key] {
				continue
			}
			anchor, found := inventoryAnchors[key]
			if !found {
				continue
			}
			viewName := "PANEL"
			if strings.EqualFold(handItem, inventoryHandNames[key]) {
				viewName = "HILITE"
			}
			props = append(props, render.FlatPropSprite{Name: inventoryHandNames[key], PropName: inventoryPropNames[key], ViewName: viewName, Anchor: anchor, Archive: inventoryArchive})
		}
		frame, projected, err := render.CompositeFlatProps(base, props)
		if err != nil {
			return fmt.Errorf("draw avatar inventory props: %w", err)
		}
		currentScene, currentPixels = 2, pixels
		currentFrame, stageFrame = frame, frame
		marieInventoryProjected = projected
		if *debug {
			log.Printf("flat=avatar props=%d handitem=%q", len(projected), handItem)
		}
		return nil
	}
	openMarieInventory := func() error {
		handFlag = 0
		marieInteractionStage = marieInteractionInventory
		return renderMarieInventory()
	}
	openIsaoInventory = func() error {
		handFlag = 0
		isaoInventoryReturnCode = isaoCurrentCode
		isaoInteractionStage = isaoInteractionInventory
		return renderMarieInventory()
	}
	var buildMarieConversationBase func() error
	startMarieSpeech = func(calls []string, nextGroup int, setPhase, putDown bool) error {
		if marieDialogue != nil {
			if err := marieDialogue.Close(); err != nil {
				return err
			}
		}
		dialogue, err := engine.NewPuppetDialogue(mariePuppet, mariePuppetTable.Entries, calls, audioContext)
		if err != nil {
			return err
		}
		frame, err := dialogue.Start(marieConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			_ = dialogue.Close()
			return err
		}
		marieDialogue, marieNextChoiceGroup, marieSetPhaseOnFinish, marieFinishNPC = dialogue, nextGroup, setPhase, putDown
		marieInteractionStage = marieInteractionPuppetSpeaking
		currentFrame, stageFrame = frame, frame
		if *debug {
			log.Printf("puppet=marie speech-lines=%d next-choice-group=%d phase-on-finish=%t putdown=%t", len(calls), nextGroup, setPhase, putDown)
		}
		return nil
	}
	finishMarieDialogue = func() error {
		if marieSetPhaseOnFinish {
			mariePhase, marieSetPhaseOnFinish = 1, false
		}
		if marieDialogue != nil {
			if err := marieDialogue.Close(); err != nil {
				return err
			}
			marieDialogue = nil
		}
		if marieNextChoiceGroup >= 0 {
			group := marieNextChoiceGroup
			marieNextChoiceGroup = -1
			return showMarieChoices(group)
		}
		marieActorValue++
		marieInteractionStage = marieInteractionIdle
		actorPoses["marie"] = "stand"
		if marieFinishNPC {
			marieVisible, marieFinishNPC = false, false
			nativeLoops.Stop(2, "marie")
		} else if marieVisible && activeSetName == "town" {
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
				return fmt.Errorf("register Marie idle loop after dialogue returned status %#x", status)
			}
		}
		if *debug {
			log.Printf("actor=marie mousedown-complete actorvalue=%d mariephase=%d visible=%t", marieActorValue, mariePhase, marieVisible)
		}
		return refreshWorldScene()
	}
	startMarieEventResponse := func(event int32) error {
		nextGroup, setPhase, putDown, occurrence := 2, false, false, 0
		switch marieChoiceGroup {
		case 0:
			if event != 102 {
				return fmt.Errorf("Marie twonite group 0 returned event %d", event)
			}
			nextGroup, occurrence = 1, 0
		case 1:
			if event != 101 {
				return fmt.Errorf("Marie twonite group 1 returned event %d", event)
			}
			nextGroup, occurrence = 2, 0
		case 2:
			switch event {
			case 203:
				marieFlag1 = true
			case 102:
				marieFlag2, occurrence = true, 1
			case 103:
				marieFlag3 = true
			case 202:
				nextGroup, setPhase, putDown = -1, true, true
			case 55555:
				if handItem == "" || handFlag == 1 {
					return openMarieInventory()
				}
				response, found := scripts.MarieGift(handItem, 0, marieGiftCounter)
				if !found {
					return fmt.Errorf("Marie gift script has no counter case %d for %q", marieGiftCounter, handItem)
				}
				giftedItem := handItem
				handItem, marieGiftCounter = "", response.Counter
				if err := buildMarieConversationBase(); err != nil {
					return err
				}
				if *debug {
					log.Printf("puppet=marie gift=%s lines=%d counter=%d", giftedItem, len(response.Speech), marieGiftCounter)
				}
				return startMarieSpeech(response.Speech, 2, false, false)
			default:
				return fmt.Errorf("Marie twonite group 2 returned event %d", event)
			}
		default:
			return fmt.Errorf("Marie twonite has unsupported choice group %d", marieChoiceGroup)
		}
		calls, err := scripts.PuppetEventSpeechCallsOccurrence(mariePuppetProgram, "twonite", event, occurrence)
		if err != nil {
			return err
		}
		return startMarieSpeech(calls, nextGroup, setPhase, putDown)
	}
	buildMarieConversationBase = func() error {
		if mariePuppet == nil {
			return fmt.Errorf("Marie PUP is unavailable")
		}
		dialogueActors := make([]render.WorldActorSprite, 0, len(worldActors))
		for _, actor := range worldActors {
			if !strings.EqualFold(actor.Name, "marie") {
				dialogueActors = append(dialogueActors, actor)
			}
		}
		dialogueBackground, _, err := compositeWorld(backgroundFrame, worldPoint, dialogueActors)
		if err != nil {
			return fmt.Errorf("hide Marie world sprite for dialogue: %w", err)
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return fmt.Errorf("render Marie dialogue panel: %w", err)
		}
		marieConversationBase, err = composeMainPanel(dialogueBackground, panel)
		if err != nil {
			return fmt.Errorf("compose Marie dialogue background: %w", err)
		}
		palette, err := mariePuppet.Palette()
		if err != nil {
			return fmt.Errorf("load Marie PUP CLUT: %w", err)
		}
		marieConversationBase.Palette = palette
		return nil
	}
	resumeMarieInventory := func() error {
		if err := buildMarieConversationBase(); err != nil {
			return err
		}
		currentFrame, stageFrame = marieConversationBase, marieConversationBase
		return showMarieChoices(2)
	}
	startMarieConversation := func() error {
		if err := openMariePuppet(); err != nil {
			return fmt.Errorf("open Marie dialogue: %w", err)
		}
		if gameClock != 3 || mariePhase != 0 {
			marieInteractionStage = marieInteractionIdle
			if *debug {
				log.Printf("puppet=marie blocked day=%d clock=%d mariephase=%d", gameDay, gameClock, mariePhase)
			}
			if marieVisible && activeSetName == "town" {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
					return fmt.Errorf("register Marie idle loop after unsupported dialogue returned status %#x", status)
				}
			}
			return nil
		}
		if err := buildMarieConversationBase(); err != nil {
			return err
		}
		marieFlag1, marieFlag2, marieFlag3 = false, false, false
		calls, err := scripts.PuppetSpeechCalls(mariePuppetProgram, "twonite", scripts.LookupOpcode("puppetclear"))
		if err != nil {
			return err
		}
		return startMarieSpeech(calls, 0, false, false)
	}
	beginMariePuppetTalk := func() (bool, error) {
		if activeSetName != "town" || !marieVisible || marieInteractionStage != marieInteractionIdle || marieWalk != nil {
			return false, nil
		}
		if gameDay == 5 || gameClock != 3 || mariePhase != 0 {
			if *debug {
				log.Printf("puppet=marie unavailable day=%d clock=%d mariephase=%d", gameDay, gameClock, mariePhase)
			}
			return false, nil
		}
		camera := render.NativeActorCameraPosition(worldPoint)
		player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		nativeLoops.Stop(2, "marie")
		marieTurnActive = false
		if scripts.NativeActorDistance2D(mariePosition, player) >= townActorHotDistance {
			destination := [3]int16{player[0], player[1], 0}
			heading := render.NativeActorHeadingToPoint(mariePosition, destination)
			walk := scripts.NewNativeActorWalkJob(mariePosition, destination, heading, townCastWalkRate)
			marieWalk, marieWalkTarget, marieInteractionStage = &walk, "", marieInteractionMoving
			if *debug {
				log.Printf("actor=marie walktopuppet destination=%v heading=%d", destination, heading)
			}
			return true, nil
		}
		actorTurnTargets["marie"] = render.NativeActorHeadingToPoint(mariePosition, player)
		marieTurnActive = actorHeadings["marie"] != actorTurnTargets["marie"]
		marieInteractionStage = marieInteractionFacing
		if !marieTurnActive {
			marieInteractionStage = marieInteractionPuppetPending
		}
		return true, nil
	}
	setupJonesBarActor := func() error {
		if !hasJonesStart || !hasJonesTarget {
			return fmt.Errorf("NITE.SET lacks town.jones1 or town.jones2")
		}
		nativeLoops.Stop(2, "jones")
		jonesPosition, jonesVisible = jonesStartPosition, true
		actorPoses["jones"] = "walk"
		actorHeadings["jones"] = render.NativeActorHeadingToPoint(jonesStartPosition, jonesTargetPosition)
		walk := scripts.NewNativeActorWalkJob(jonesStartPosition, jonesTargetPosition, actorHeadings["jones"], jonesWalkRate)
		jonesWalk = &walk
		if *debug {
			log.Printf("actor=jones setup=bar set=town start=%v target=%v scale=1450 speed=%d", jonesStartPosition, jonesTargetPosition, jonesWalkRate)
		}
		return nil
	}
	finishHelpChoice := func() error {
		if helpPendingResult.SetPhase {
			helpPhase = helpPendingResult.Phase
		}
		if helpPendingResult.GiveRing {
			inventoryOwners["ring"], handItem = "stranger", "ring"
			if err := soundBank.Play(audioContext, "inven", 1); err != nil {
				return fmt.Errorf("play Ring inventory sound: %w", err)
			}
		}
		if helpPendingResult.HideHelp {
			helpVisible = false
			helpWalk = nil
			helpInteractionStage = helpInteractionIdle
			nativeLoops.Stop(2, "help")
		}
		if helpPendingResult.GiveRing && helpPendingResult.HideHelp {
			if err := setupJonesBarActor(); err != nil {
				return fmt.Errorf("setup Jones after Ring handoff: %w", err)
			}
		}
		if helpPendingResult.GiveBone {
			boneWorldProp.Position, boneWorldProp.Heading, boneWorldProp.Scale = bonePosition, 32, 1200
			boneWorldProp.View, boneWorldProp.Visible, boneOwner = boneSmallView, true, "none"
			if *debug {
				log.Printf("prop=Bone setup=street owner=%s view=small point=%v degree=%d scale=%d", boneOwner, boneWorldProp.Position, boneWorldProp.Heading, boneWorldProp.Scale)
			}
			if err := refreshWorldScene(); err != nil {
				return fmt.Errorf("show Bone after Help dialogue: %w", err)
			}
		}
		if helpPendingResult.NextPage != "" {
			nextPage := helpPendingResult.NextPage
			helpPendingResult = scripts.HelpChoiceResult{}
			return startHelpPage(nextPage)
		}
		if helpPendingResult.Complete {
			if helpPendingResult.HideHelp {
				helpPendingResult = scripts.HelpChoiceResult{}
				return refreshWorldScene()
			}
			return returnHelpToStar()
		}
		return fmt.Errorf("HELP1.PUP page %s has no verified continuation", helpPage)
	}
	startHelpChoice := func(event int32) error {
		result, found := scripts.HelpPuppetChoice(helpPage, event)
		if !found {
			return fmt.Errorf("HELP1.PUP %s returned unsupported event %d", helpPage, event)
		}
		helpPendingResult, helpSpeechStages, helpSpeechStageIndex = result, result.Speech, 0
		helpInitialPage, helpDelayReady = false, false
		if len(helpSpeechStages) == 0 {
			return finishHelpChoice()
		}
		return startHelpSpeechStage()
	}
	runNativeScheduler := func(serviceAmbient bool) (bool, error) {
		displayChanged := false
		status, err := nativeLoops.PassWhere(func(loop scripts.ScriptLoop) (uint16, error) {
			switch loop.Callback {
			case "toidle", "leroyidle":
				actorCamera := render.NativeActorCameraPosition(worldPoint)
				playerPoint := [3]int16{int16(actorCamera[0]), int16(actorCamera[1]), int16(actorCamera[2])}
				dx, dy, dz := int(leroyPosition[0])-int(playerPoint[0]), int(leroyPosition[1])-int(playerPoint[1]), int(leroyPosition[2])-int(playerPoint[2])
				step, found := scripts.LeroyIdleStep(loop.Callback, dx*dx+dy*dy+dz*dz < 384*384, true, leroyPhase, &nativeRandom)
				if !found {
					return 0, fmt.Errorf("unknown Leroy idle callback %q", loop.Callback)
				}
				actorPoses["leroy"], loop.Callback, loop.Remaining = step.Pose, step.Callback, step.Remaining
				if step.TurnToCamera {
					actorTurnTargets["leroy"] = render.NativeActorHeadingToPoint(leroyPosition, playerPoint)
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
			case "helpidle":
				camera := render.NativeActorCameraPosition(worldPoint)
				player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
				dogVisible := false
				for _, actor := range worldActors {
					if strings.EqualFold(actor.Name, "dog") && actor.Visible {
						dogVisible = true
						break
					}
				}
				step, found := scripts.HelpIdleStep(loop.Callback, scripts.NativeActorDistance2D(helpPosition, player) < 384, dogVisible, gameDay, helpPhase)
				if !found {
					return 0, fmt.Errorf("unknown Help idle callback %q", loop.Callback)
				}
				previousPose := actorPoses["help"]
				actorPoses["help"], loop.Callback, loop.Remaining = step.Pose, step.Callback, step.Remaining
				if step.ClearAttention {
					helpAttention = 0
				}
				if step.Attention != 0 {
					helpAttention = step.Attention
				}
				if step.TurnToCamera {
					actorTurnTargets["help"] = render.NativeActorHeadingToPoint(helpPosition, player)
					helpTurnActive = actorHeadings["help"] != actorTurnTargets["help"]
				}
				displayChanged = displayChanged || currentScene == 0 && previousPose != step.Pose
				if *debug {
					log.Printf("actor=help pose=%s next=%s ticks=%d attention=%d", step.Pose, step.Callback, step.Remaining, helpAttention)
				}
			case "buickidle", "marieidle":
				name := strings.TrimSuffix(loop.Callback, "idle")
				star, position, moving, visible := "", [3]int16{}, false, false
				interval := int32(17)
				switch name {
				case "buick":
					star, position, moving, visible, interval = buickStar, buickPosition, buickWalk != nil, buickVisible, 21
				case "marie":
					star, position, moving, visible = marieStar, mariePosition, marieWalk != nil, marieVisible
				}
				if !visible {
					return 0, fmt.Errorf("%s idle loop ran while actor is hidden", name)
				}
				loop.Remaining = interval
				if moving {
					break
				}
				step, found := scripts.TownCastIdleStep(loop.Callback, star, &nativeRandom)
				if !found {
					return 0, fmt.Errorf("unknown Town cast idle callback %q", loop.Callback)
				}
				loop.Remaining = step.Remaining
				camera := render.NativeActorCameraPosition(worldPoint)
				player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
				if scripts.NativeActorDistance2D(position, player) < townActorHotDistance {
					actorTurnTargets[name] = render.NativeActorHeadingToPoint(position, player)
					if name == "buick" {
						buickTurnActive = actorHeadings[name] != actorTurnTargets[name]
					} else {
						marieTurnActive = actorHeadings[name] != actorTurnTargets[name]
					}
				}
				if step.Star != star {
					var destination [3]int16
					var found bool
					switch step.Star {
					case "town.blood1":
						destination, found = buickPosition, hasBuick
					case "town.blood2":
						destination, found = buickSecondPosition, hasBuickSecond
					case "town.jones1":
						destination, found = jonesStartPosition, hasJonesStart
					case "town.jones2":
						destination, found = jonesTargetPosition, hasJonesTarget
					case "town.marie1":
						destination, found = marieSecondPosition, hasMarieSecond
					}
					if !found {
						return 0, fmt.Errorf("%s idle selected missing Town star %q", name, step.Star)
					}
					heading := render.NativeActorHeadingToPoint(position, destination)
					walk := scripts.NewNativeActorWalkJob(position, destination, heading, townCastWalkRate)
					if name == "buick" {
						buickWalk, buickWalkTarget, actorPoses[name] = &walk, step.Star, "walk"
					} else {
						marieWalk, marieWalkTarget, actorPoses[name] = &walk, step.Star, "walk"
					}
					displayChanged = displayChanged || currentScene == 0
					if *debug {
						log.Printf("actor=%s idle-move from=%s to=%s ticks=%d", name, star, step.Star, step.Remaining)
					}
				}
			case "jonesidle":
				camera := render.NativeActorCameraPosition(worldPoint)
				player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
				if scripts.NativeActorDistance2D(jonesPosition, player) < 384 {
					actorTurnTargets["jones"] = render.NativeActorHeadingToPoint(jonesPosition, player)
					jonesTurnActive = actorHeadings["jones"] != actorTurnTargets["jones"]
				}
				loop.Remaining = 17
				displayChanged = displayChanged || currentScene == 0
				if *debug {
					log.Printf("actor=jones idle=turn-check point=%v ticks=%d turn=%t", jonesPosition, loop.Remaining, jonesTurnActive)
				}
			case "isaoidle":
				if isaoBouncer {
					actorPoses["isao"], isaoBouncer = "stand", false
				} else {
					actorPoses["isao"], isaoBouncer = "up", true
				}
				if isaoDirGo {
					actorHeadings["isao"] -= 2
					if actorHeadings["isao"] < 44 {
						isaoDirGo = false
					}
				} else {
					actorHeadings["isao"] += 2
					if actorHeadings["isao"] > 84 {
						isaoDirGo = true
					}
				}
				loop.Remaining = 2
				displayChanged = displayChanged || currentScene == 0
				if *debug {
					log.Printf("actor=isao pose=%s heading=%d next=%s ticks=%d", actorPoses["isao"], actorHeadings["isao"], loop.Callback, loop.Remaining)
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
		if !serviceAmbient && helpTurnActive {
			actorHeadings["help"] = scripts.NativeTurnStep(actorHeadings["help"], actorTurnTargets["help"], helpTurnRate)
			helpTurnActive = actorHeadings["help"] != actorTurnTargets["help"]
			displayChanged = displayChanged || currentScene == 0
			if *debug {
				log.Printf("actor=help turn heading=%d target=%d active=%t", actorHeadings["help"], actorTurnTargets["help"], helpTurnActive)
			}
		}
		if !serviceAmbient && jonesTurnActive {
			actorHeadings["jones"] = scripts.NativeTurnStep(actorHeadings["jones"], actorTurnTargets["jones"], jonesTurnRate)
			jonesTurnActive = actorHeadings["jones"] != actorTurnTargets["jones"]
			displayChanged = displayChanged || currentScene == 0
			if *debug {
				log.Printf("actor=jones turn heading=%d target=%d active=%t", actorHeadings["jones"], actorTurnTargets["jones"], jonesTurnActive)
			}
		}
		if !serviceAmbient && buickTurnActive {
			actorHeadings["buick"] = scripts.NativeTurnStep(actorHeadings["buick"], actorTurnTargets["buick"], jonesTurnRate)
			buickTurnActive = actorHeadings["buick"] != actorTurnTargets["buick"]
			displayChanged = displayChanged || currentScene == 0
		}
		if !serviceAmbient && marieTurnActive {
			actorHeadings["marie"] = scripts.NativeTurnStep(actorHeadings["marie"], actorTurnTargets["marie"], jonesTurnRate)
			marieTurnActive = actorHeadings["marie"] != actorTurnTargets["marie"]
			displayChanged = displayChanged || currentScene == 0
		}
		if !serviceAmbient && marieInteractionStage == marieInteractionFacing && !marieTurnActive {
			marieInteractionStage = marieInteractionPuppetPending
		}
		if !serviceAmbient && jonesInteractionStage == jonesInteractionFacing && !jonesTurnActive {
			jonesInteractionStage = jonesInteractionPuppetPending
		}
		if !serviceAmbient && helpInteractionStage == helpInteractionFacing && !helpTurnActive {
			helpInteractionStage = helpInteractionPuppetPending
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
		if helpWalk != nil {
			previousPosition, previousHeading := helpActorPosition, actorHeadings["help"]
			var walking bool
			helpActorPosition, actorHeadings["help"], walking = helpWalk.Pass(helpActorPosition, actorHeadings["help"], helpTurnRate)
			displayChanged = displayChanged || helpActorPosition != previousPosition || actorHeadings["help"] != previousHeading
			if *debug && (helpActorPosition != previousPosition || actorHeadings["help"] != previousHeading) {
				log.Printf("actor=help walk point=%v heading=%d active=%t", helpActorPosition, actorHeadings["help"], walking)
			}
			if !walking {
				helpWalk = nil
				if helpInteractionStage == helpInteractionMoving {
					currentDegree, found := render.NativeCurrentDegree(worldPoint[2])
					if !found {
						return false, fmt.Errorf("NITE.SET orientation %d has no native currentdeg", worldPoint[2])
					}
					actorTurnTargets["help"] = int16((int(currentDegree) + 128) % 256)
					helpTurnActive = actorHeadings["help"] != actorTurnTargets["help"]
					helpInteractionStage = helpInteractionFacing
					if !helpTurnActive {
						helpInteractionStage = helpInteractionPuppetPending
					}
				} else if helpInteractionStage == helpInteractionReturning {
					if err := completeHelpReturn(); err != nil {
						return false, fmt.Errorf("resume Help idle callback: %w", err)
					}
					displayChanged = true
				}
			}
		}
		if jonesWalk != nil {
			previousPosition, previousHeading := jonesPosition, actorHeadings["jones"]
			var walking bool
			jonesPosition, actorHeadings["jones"], walking = jonesWalk.Pass(jonesPosition, actorHeadings["jones"], jonesTurnRate)
			displayChanged = displayChanged || jonesPosition != previousPosition || actorHeadings["jones"] != previousHeading
			if *debug && (jonesPosition != previousPosition || actorHeadings["jones"] != previousHeading) {
				log.Printf("actor=jones walk point=%v heading=%d active=%t", jonesPosition, actorHeadings["jones"], walking)
			}
			if !walking {
				jonesWalk = nil
				actorPoses["jones"] = "stand"
				if jonesInteractionStage == jonesInteractionMoving {
					camera := render.NativeActorCameraPosition(worldPoint)
					player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
					actorTurnTargets["jones"] = render.NativeActorHeadingToPoint(jonesPosition, player)
					jonesTurnActive = actorHeadings["jones"] != actorTurnTargets["jones"]
					jonesInteractionStage = jonesInteractionFacing
					if !jonesTurnActive {
						jonesInteractionStage = jonesInteractionPuppetPending
					}
				} else if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
					return false, fmt.Errorf("register Jones idle loop returned status %#x", status)
				}
				displayChanged = true
				if *debug {
					log.Printf("actor=jones endwalk point=%v pose=stand idle=jonesidle", jonesPosition)
				}
			}
		}
		if buickWalk != nil {
			previousPosition, previousHeading := buickPosition, actorHeadings["buick"]
			var walking bool
			buickPosition, actorHeadings["buick"], walking = buickWalk.Pass(buickPosition, actorHeadings["buick"], jonesTurnRate)
			displayChanged = displayChanged || buickPosition != previousPosition || actorHeadings["buick"] != previousHeading
			if !walking {
				buickWalk = nil
				buickStar, buickWalkTarget, actorPoses["buick"] = buickWalkTarget, "", "stand"
				if *debug {
					log.Printf("actor=buick endwalk star=%s point=%v", buickStar, buickPosition)
				}
			}
		}
		if marieWalk != nil {
			previousPosition, previousHeading := mariePosition, actorHeadings["marie"]
			var walking bool
			mariePosition, actorHeadings["marie"], walking = marieWalk.Pass(mariePosition, actorHeadings["marie"], jonesTurnRate)
			displayChanged = displayChanged || mariePosition != previousPosition || actorHeadings["marie"] != previousHeading
			if !walking {
				marieWalk = nil
				actorPoses["marie"] = "stand"
				if marieInteractionStage == marieInteractionMoving {
					camera := render.NativeActorCameraPosition(worldPoint)
					player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
					actorTurnTargets["marie"] = render.NativeActorHeadingToPoint(mariePosition, player)
					marieTurnActive = actorHeadings["marie"] != actorTurnTargets["marie"]
					marieInteractionStage = marieInteractionFacing
					if !marieTurnActive {
						marieInteractionStage = marieInteractionPuppetPending
					}
				} else {
					marieStar, marieWalkTarget = marieWalkTarget, ""
				}
				if *debug {
					log.Printf("actor=marie endwalk star=%s point=%v", marieStar, mariePosition)
				}
			}
		}
		if displayChanged {
			if err := refreshWorldScene(); err != nil {
				return false, err
			}
		}
		if !serviceAmbient && helpInteractionStage == helpInteractionPuppetPending {
			if err := startHelpPuppet(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !serviceAmbient && helpInteractionStage == helpInteractionPuppetSpeaking {
			if helpDialogue == nil {
				return false, fmt.Errorf("Help dialogue state is missing its puppet player")
			}
			frameTick := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			var frame render.IndexedFrame
			var changed bool
			var err error
			if helpDialogueSkip {
				frame, changed, err = helpDialogue.Skip()
				helpDialogueSkip = false
			} else {
				frame, changed, err = helpDialogue.Update(frameTick)
			}
			if err != nil {
				return false, fmt.Errorf("advance Help dialogue: %w", err)
			}
			if changed {
				currentFrame, stageFrame = frame, frame
				if !helpDialogue.Active() {
					if helpInitialPage {
						helpInitialPage = false
						if err := showHelpChoices(helpPage); err != nil {
							return false, fmt.Errorf("show Help choices: %w", err)
						}
					} else if helpSpeechStageIndex+1 < len(helpSpeechStages) {
						helpSpeechStageIndex++
						if err := startHelpSpeechStage(); err != nil {
							return false, fmt.Errorf("start Help speech stage: %w", err)
						}
					} else if err := finishHelpChoice(); err != nil {
						return false, fmt.Errorf("finish Help choice: %w", err)
					}
				}
				return true, nil
			}
			if displayChanged {
				frame, err = helpDialogue.Frame()
				if err != nil {
					return false, fmt.Errorf("refresh Help dialogue frame: %w", err)
				}
				currentFrame, stageFrame = frame, frame
				return true, nil
			}
		}
		if !serviceAmbient && helpInteractionStage == helpInteractionPuppetDelay && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= helpDelayUntil {
			helpDelayReady = true
			if err := startHelpSpeechStage(); err != nil {
				return false, fmt.Errorf("continue delayed Help speech: %w", err)
			}
			return true, nil
		}
		if !serviceAmbient && displayChanged && helpInteractionStage == helpInteractionPuppetChoices {
			if err := showHelpChoices(helpPage); err != nil {
				return false, fmt.Errorf("refresh Help choice panel: %w", err)
			}
			return true, nil
		}
		if !serviceAmbient && isaoInteractionStage == isaoInteractionPuppetPending {
			if err := startIsaoConversation(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !serviceAmbient && isaoInteractionStage == isaoInteractionPuppetSpeaking {
			if isaoDialogue == nil {
				return false, fmt.Errorf("Isao dialogue state is missing its puppet player")
			}
			frameTick := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			var frame render.IndexedFrame
			var changed bool
			var err error
			if isaoDialogueSkip {
				frame, changed, err = isaoDialogue.Skip()
				isaoDialogueSkip = false
			} else {
				frame, changed, err = isaoDialogue.Update(frameTick)
			}
			if err != nil {
				return false, fmt.Errorf("advance Isao dialogue: %w", err)
			}
			if changed {
				currentFrame, stageFrame = frame, frame
				if !isaoDialogue.Active() {
					if err := finishIsaoSpeech(); err != nil {
						return false, fmt.Errorf("finish Isao dialogue: %w", err)
					}
				}
				return true, nil
			}
			if displayChanged {
				frame, err = isaoDialogue.Frame()
				if err != nil {
					return false, fmt.Errorf("refresh Isao dialogue frame: %w", err)
				}
				currentFrame, stageFrame = frame, frame
				return true, nil
			}
		}
		if !serviceAmbient && displayChanged && isaoInteractionStage == isaoInteractionPuppetChoices {
			if err := drawIsaoChoices(isaoChoiceOutline); err != nil {
				return false, fmt.Errorf("refresh Isao choices: %w", err)
			}
			return true, nil
		}
		if !serviceAmbient && isaoInteractionStage == isaoInteractionPuppetDelay && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= isaoDelayUntil {
			if err := startIsaoConversation(); err != nil {
				return false, fmt.Errorf("start Isao follow-up: %w", err)
			}
			return true, nil
		}
		if !serviceAmbient && jonesInteractionStage == jonesInteractionPuppetPending {
			if err := startJonesConversation(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !serviceAmbient && jonesInteractionStage == jonesInteractionPuppetSpeaking {
			if jonesDialogue == nil {
				return false, fmt.Errorf("Jones dialogue state is missing its puppet player")
			}
			frameTick := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			var frame render.IndexedFrame
			var changed bool
			var err error
			if jonesDialogueSkip {
				frame, changed, err = jonesDialogue.Skip()
				jonesDialogueSkip = false
			} else {
				frame, changed, err = jonesDialogue.Update(frameTick)
			}
			if err != nil {
				return false, fmt.Errorf("advance Jones dialogue: %w", err)
			}
			if changed {
				currentFrame, stageFrame = frame, frame
				if !jonesDialogue.Active() {
					if err := finishJonesDialogue(); err != nil {
						return false, fmt.Errorf("finish Jones dialogue: %w", err)
					}
				}
				return true, nil
			}
			if displayChanged {
				frame, err = jonesDialogue.Frame()
				if err != nil {
					return false, fmt.Errorf("refresh Jones dialogue frame: %w", err)
				}
				currentFrame, stageFrame = frame, frame
				return true, nil
			}
		}
		if !serviceAmbient && displayChanged && jonesInteractionStage == jonesInteractionPuppetChoices {
			if err := showJonesChoices(jonesChoiceGroup); err != nil {
				return false, fmt.Errorf("refresh Jones choice panel: %w", err)
			}
			return true, nil
		}
		if !serviceAmbient && marieInteractionStage == marieInteractionPuppetPending {
			if err := startMarieConversation(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !serviceAmbient && marieInteractionStage == marieInteractionPuppetSpeaking {
			if marieDialogue == nil {
				return false, fmt.Errorf("Marie dialogue state is missing its puppet player")
			}
			frameTick := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			var frame render.IndexedFrame
			var changed bool
			var err error
			if marieDialogueSkip {
				frame, changed, err = marieDialogue.Skip()
				marieDialogueSkip = false
			} else {
				frame, changed, err = marieDialogue.Update(frameTick)
			}
			if err != nil {
				return false, fmt.Errorf("advance Marie dialogue: %w", err)
			}
			if changed {
				currentFrame, stageFrame = frame, frame
				if !marieDialogue.Active() {
					if err := finishMarieDialogue(); err != nil {
						return false, fmt.Errorf("finish Marie dialogue: %w", err)
					}
				}
				return true, nil
			}
			if displayChanged {
				frame, err = marieDialogue.Frame()
				if err != nil {
					return false, fmt.Errorf("refresh Marie dialogue frame: %w", err)
				}
				currentFrame, stageFrame = frame, frame
				return true, nil
			}
		}
		if !serviceAmbient && displayChanged && marieInteractionStage == marieInteractionPuppetChoices {
			if err := showMarieChoices(marieChoiceGroup); err != nil {
				return false, fmt.Errorf("refresh Marie choice panel: %w", err)
			}
			return true, nil
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
	dogMoviePanelFrame := render.IndexedFrame{}
	dogMoviePanelTop, dogMoviePanelActive := 0, false
	movieOutputFrame := func(frame render.IndexedFrame) (render.IndexedFrame, error) {
		if !dogMoviePanelActive {
			return frame, nil
		}
		return render.CompositePanel(frame, dogMoviePanelFrame, dogMoviePanelTop)
	}
	startSceneMovie := func(name string) error {
		if currentScene == 0 {
			if err := refreshWorldScene(); err != nil {
				return fmt.Errorf("refresh scene before movie %s: %w", name, err)
			}
		}
		dogMoviePanelActive = strings.EqualFold(name, "MOVIES/DOG1.MOV") || strings.EqualFold(name, "MOVIES/DOG2.MOV")
		if dogMoviePanelActive {
			dogMoviePanelFrame, dogMoviePanelTop = stageFrame, backgroundFrame.Height
		}
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
		movieRestorePalette = activeSet.Palette()
		playback, err = render.NewMoviePlayback(movie, currentFrame, movieRestorePalette)
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
	var pendingSceneMovie string
	var dogMovieNeedsHelp bool
	const (
		dog2OfferIdle uint8 = iota
		dog2OfferMovie
		dog2OfferDelayBeforeEast
		dog2OfferWaitEast
		dog2OfferDelayAfterEast
	)
	dog2OfferStage := dog2OfferIdle
	var dog2OfferUntil uint32
	nativeCursor := ""
	setNativeCursor := func(name string) {
		if *silent || nativeCursor == name {
			return
		}
		nativeCursor = name
		if strings.EqualFold(name, "touch") {
			ebiten.SetCursorShape(ebiten.CursorShapePointer)
		} else {
			ebiten.SetCursorShape(ebiten.CursorShapeDefault)
		}
	}
	updateNativeCursor := func(point uint32) {
		cursor := "arrow"
		if currentScene == 0 {
			if _, found, _ := stage.HitTestSceneHandler(currentScene, point); found {
				cursor = "touch"
			}
			if _, found := render.HitTestWorldActors(projectedActors, image.Pt(int(int16(point>>16)), int(int16(point)))); found {
				cursor = "touch"
			}
			if strings.EqualFold(string(view.Name[1:]), "Scene G15") {
				if _, found := scripts.NiteNorthObjectAction(worldPoint[2], point, gameClock); found {
					cursor = "touch"
				}
			}
			if activeSetName == "town" {
				if _, found := scripts.NiteDoorAt(view.Resource, worldPoint[2], point); found {
					cursor = "touch"
				}
			} else if activeSetName == "sallower" {
				if _, found := scripts.SallowerDoorAt(view.Resource, worldPoint[2], point); found {
					cursor = "touch"
				}
			} else if activeSetName == "hotlower" {
				if _, found := scripts.HotLowerDoorAt(view.Resource, worldPoint[2], point); found {
					cursor = "touch"
				}
			}
			if helpInteractionStage == helpInteractionPuppetChoices {
				if _, found := scripts.NativePuppetChoiceAt(point, helpActiveChoices); found {
					cursor = "touch"
				}
			}
			if leroyInteractionStage == leroyInteractionPuppetChoices {
				if _, found := scripts.NativePuppetChoiceAt(point, leroyActiveChoices); found {
					cursor = "touch"
				}
			}
			if jonesInteractionStage == jonesInteractionPuppetChoices {
				if _, found := scripts.NativePuppetChoiceAt(point, jonesActiveChoices); found {
					cursor = "touch"
				}
			}
			if isaoInteractionStage == isaoInteractionPuppetChoices {
				if _, found := scripts.NativePuppetChoiceAt(point, isaoActiveChoices); found {
					cursor = "touch"
				}
			}
			if marieInteractionStage == marieInteractionPuppetChoices {
				if _, found := scripts.NativePuppetChoiceAt(point, marieActiveChoices); found {
					cursor = "touch"
				}
			}
			if (marieInteractionStage == marieInteractionInventory || isaoInteractionStage == isaoInteractionInventory) && currentScene == 2 {
				if _, found := render.HitTestFlatProps(marieInventoryProjected, image.Pt(int(int16(point>>16)), int(int16(point)))); found {
					cursor = "touch"
				}
			}
		} else if _, found, _ := stage.HitTestSceneHandler(currentScene, point); found {
			cursor = "touch"
		}
		setNativeCursor(cursor)
	}
	var transition interface {
		CurrentFrame() render.IndexedFrame
		TargetFrame() render.IndexedFrame
		Update() (render.IndexedFrame, bool, bool)
	}
	var transitionMode uint8
	var pendingSetName, pendingSetScene, pendingSetDirection string
	var transferSetName, transferSetScene, transferSetDirection string
	runErr := engine.Run(playback.CurrentFrame(), func() (render.IndexedFrame, bool, error) {
		if playback == nil && transition == nil && pendingSetName != "" {
			transferSetName, transferSetScene, transferSetDirection = pendingSetName, pendingSetScene, pendingSetDirection
			pendingSetName, pendingSetScene, pendingSetDirection = "", "", ""
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, 30)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("fade before SET transfer: %w", err)
			}
			transition, transitionMode = fade, 1
			if *debug {
				log.Printf("set-transfer=fade-out target=%s scene=%q direction=%q duration=30", transferSetName, transferSetScene, transferSetDirection)
			}
			return fade.CurrentFrame(), true, nil
		}
		if playback == nil && transition == nil && pendingSceneMovie != "" {
			name := pendingSceneMovie
			pendingSceneMovie = ""
			if err := startSceneMovie(name); err != nil {
				return render.IndexedFrame{}, false, err
			}
			frame, err := movieOutputFrame(playback.CurrentFrame())
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			return frame, true, nil
		}
		if playback == nil && transition == nil {
			changed, err := runNativeScheduler(false)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if changed {
				return currentFrame, true, nil
			}
		}
		if playback == nil && transition == nil {
			now := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			if dog2OfferStage == dog2OfferDelayBeforeEast && now >= dog2OfferUntil {
				dog2OfferStage, pendingMovement = dog2OfferWaitEast, assets.SceneMoveRight
				if *debug {
					log.Printf("actor=dog offerobject=turn direction=east movement=right")
				}
			}
			if dog2OfferStage == dog2OfferWaitEast && pendingMovement == 0 && worldPoint[2] == assets.SetDirectionEast {
				dog2OfferStage, dog2OfferUntil = dog2OfferDelayAfterEast, now+60
				if *debug {
					log.Printf("actor=dog offerobject=east wait=60")
				}
			}
			if dog2OfferStage == dog2OfferDelayAfterEast && now >= dog2OfferUntil {
				if err := setupHelpActor(); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("setup Help after DOG2.MOV: %w", err)
				}
				if err := refreshWorldScene(); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("show Help after DOG2.MOV: %w", err)
				}
				started, err := beginHelpPuppetTalk()
				if err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("run Help mousedown after DOG2.MOV: %w", err)
				}
				dog2OfferStage = dog2OfferIdle
				if started {
					dog2PhaseAfterHelp = true
				} else {
					gamePhase = 2
				}
				if *debug {
					log.Printf("actor=dog offerobject=help-mousedown started=%t phase-pending=%t", started, dog2PhaseAfterHelp)
				}
				return currentFrame, true, nil
			}
		}
		if playback == nil && transition == nil && currentScene == 0 && pendingMovement != 0 {
			movement := pendingMovement
			pendingMovement = 0
			nextPoint, transitionResource, found, err := activeSet.MovePoint(worldPoint, movement)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("move in %s: %w", activeSetName, err)
			}
			if !found {
				if *debug {
					log.Printf("level-move blocked movement=%d point=%v", movement, worldPoint)
				}
				return render.IndexedFrame{}, false, nil
			}
			viewPoint := assets.SetView{DirectionID: uint16(nextPoint[0]), SceneID: uint16(nextPoint[1])}
			frameResource, hasView, err := activeSet.BackgroundResourceForDirection(viewPoint, nextPoint[2])
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("resolve %s directional background after movement: %w", activeSetName, err)
			}
			nextView, hasEventView := activeSet.FindViewByIDs(viewPoint.DirectionID, viewPoint.SceneID)
			if hasView && !hasEventView {
				return render.IndexedFrame{}, false, fmt.Errorf("%s has no event view for scene IDs %d,%d", activeSetName, viewPoint.DirectionID, viewPoint.SceneID)
			}
			if !hasView {
				frameResource = transitionResource
			}
			backgroundData, err := activeSet.Resource(frameResource)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("load %s background %d: %w", activeSetName, frameResource, err)
			}
			backgroundPixels, decodeErr := render.DecodeMoviePixels(backgroundData, nil)
			if len(backgroundPixels.Pixels) == 0 {
				return render.IndexedFrame{}, false, fmt.Errorf("decode NITE.SET background %d: %w", frameResource, decodeErr)
			}
			if decodeErr != nil && *debug {
				log.Printf("level-resource=%d partial-frame: %v", frameResource, decodeErr)
			}
			nextBackground, err := render.StageFrame(&assets.Stage{Width: uint16(backgroundPixels.Width), Height: uint16(backgroundPixels.Height), PaletteRaw: activeSet.Palette()}, backgroundPixels.Pixels)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("render %s background %d: %w", activeSetName, frameResource, err)
			}
			nextActors, err := loadWorldActors(nextPoint)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			nextWorldBackground, nextProjectedActors, err := compositeWorld(nextBackground, nextPoint, nextActors)
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
			if hasEventView {
				view = nextView
			} else {
				view = assets.SetView{}
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
				if transitionMode == 1 {
					blackFrame := transition.TargetFrame()
					nextFrame, err := switchSpecialSet(transferSetName, transferSetScene, transferSetDirection)
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					fade, err := render.NewFadeEffect(blackFrame, nextFrame, 30)
					if err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("fade into SET %s: %w", transferSetName, err)
					}
					transition, transitionMode = fade, 2
					if *debug {
						log.Printf("set-transfer=fade-in set=%s scene=%s duration=30", activeSetName, view.Name[1:])
					}
					return fade.CurrentFrame(), true, nil
				}
				currentFrame, transition = transition.TargetFrame(), nil
				if transitionMode == 2 {
					stageFrame = currentFrame
					transitionMode = 0
					if isaoInventoryReturnPending {
						isaoInventoryReturnPending = false
						if err := resumeIsaoInventory(); err != nil {
							return render.IndexedFrame{}, false, err
						}
						return currentFrame, true, nil
					}
					if marieInventoryReturnPending {
						marieInventoryReturnPending = false
						if err := resumeMarieInventory(); err != nil {
							return render.IndexedFrame{}, false, err
						}
						return currentFrame, true, nil
					}
					return currentFrame, true, nil
				}
			}
			return transitionFrame, changed, nil
		}
		movieFrame, changed, done, err := playback.Update()
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("advance startup movie %s: %w", movieNames[movieIndex], err)
		}
		movieFrame, err = movieOutputFrame(movieFrame)
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("restore Dog movie panel: %w", err)
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
		dogMoviePanelActive = false
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
			playback, err = render.NewMoviePlayback(movie, movieFrame, movieRestorePalette)
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
		if dogMovieNeedsHelp {
			dogMovieNeedsHelp = false
			if err := setupHelpActor(); err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("setup Help after DOG1.MOV: %w", err)
			}
			if err := refreshWorldScene(); err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("show Help after DOG1.MOV: %w", err)
			}
			return currentFrame, true, nil
		}
		if dog2OfferStage == dog2OfferMovie {
			dog2OfferStage = dog2OfferDelayBeforeEast
			dog2OfferUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + 60
			if *debug {
				log.Printf("actor=dog offerobject=movie-complete wait=east frames=60")
			}
			return currentFrame, true, nil
		}
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
			}
			if gameDay == 1 {
				step, found := scripts.DogIdleStep("doright", &nativeRandom)
				if found {
					actorPoses["dog"] = step.Pose
					if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "dog", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
						return render.IndexedFrame{}, false, fmt.Errorf("register dog idle loop returned status %#x", status)
					}
				}
			}
			if buickVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "buick", Callback: "buickidle", Remaining: 21}); status != 0 {
					return render.IndexedFrame{}, false, fmt.Errorf("register Buick idle loop returned status %#x", status)
				}
			}
			if marieVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: 2, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
					return render.IndexedFrame{}, false, fmt.Errorf("register Marie idle loop returned status %#x", status)
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
		if helpInteractionStage == helpInteractionPuppetSpeaking {
			if key == ebiten.KeySpace || key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				helpDialogueSkip = true
			}
			return
		}
		if helpInteractionStage != helpInteractionIdle && helpInteractionStage != helpInteractionPuppetChoices {
			return
		}
		if helpInteractionStage == helpInteractionPuppetChoices {
			return
		}
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
		if isaoInteractionStage == isaoInteractionPuppetSpeaking {
			if key == ebiten.KeySpace || key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				isaoDialogueSkip = true
			}
			return
		}
		if isaoInteractionStage != isaoInteractionIdle {
			return
		}
		if jonesInteractionStage == jonesInteractionPuppetSpeaking {
			if key == ebiten.KeySpace || key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				jonesDialogueSkip = true
			}
			return
		}
		if jonesInteractionStage != jonesInteractionIdle {
			return
		}
		if marieInteractionStage == marieInteractionPuppetSpeaking {
			if key == ebiten.KeySpace || key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				marieDialogueSkip = true
			}
			return
		}
		if marieInteractionStage != marieInteractionIdle {
			return
		}
		if pendingSceneMovie != "" {
			return
		}
		if playback == nil {
			if currentScene == 0 && transition == nil {
				switch key {
				case ebiten.KeyArrowUp:
					if activeSetName == "town" {
						if target, found := scripts.NiteInteriorTarget(view.Resource, worldPoint[2], doorOwner); found {
							pendingSetName, doorOwner = target, ""
							if *debug {
								log.Printf("interior-enter target=%s view=%s point=%v", target, view.Name[1:], worldPoint)
							}
							return
						}
					} else {
						direction := ""
						switch activeSetName {
						case "sallower":
							if scripts.SallowerExitToTown(worldPoint[2], doorOwner) {
								direction = "east"
							}
						case "hotlower":
							direction, _ = scripts.HotLowerExitToTown(view.Resource, worldPoint[2], doorOwner)
						case "store", "livery":
							if worldPoint[2] == assets.SetDirectionEast && (activeSetName == "store" && doorOwner == "shop" || activeSetName == "livery" && doorOwner == "horse") {
								direction = "west"
							}
						}
						if direction != "" {
							pendingSetName, pendingSetScene, pendingSetDirection, doorOwner = "nite.set", townReturnScene, direction, ""
							if gameClock != 3 {
								pendingSetName = "town.set"
							}
							if *debug {
								log.Printf("interior-exit set=%s scene=%q direction=%s", activeSetName, townReturnScene, direction)
							}
							return
						}
					}
					dogVisible := false
					for _, actor := range worldActors {
						if strings.EqualFold(actor.Name, "dog") && actor.Visible {
							dogVisible = true
							break
						}
					}
					if name, blocked := scripts.NiteDogGateMovieInView(view.Resource, worldPoint[2], gameDay, dogVisible); blocked {
						pendingMovement = 0
						pendingSceneMovie = name
						dogMovieNeedsHelp = true
						if *debug {
							log.Printf("event=NITE.SET/key-down dog-gate point=%v movie=%s", worldPoint, name)
						}
						return
					}
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
		if helpInteractionStage != helpInteractionIdle && helpInteractionStage != helpInteractionPuppetChoices {
			return currentFrame, false, nil
		}
		if helpInteractionStage == helpInteractionPuppetChoices {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			event, found := scripts.NativePuppetChoiceAt(point, helpActiveChoices)
			if !found {
				return currentFrame, false, nil
			}
			helpChoicePressActive, helpChoicePressEvent = true, event
			helpChoicePressIndex = (int(int16(point)) - 264) / 24
			return currentFrame, false, nil
		}
		if jonesInteractionStage == jonesInteractionPuppetChoices {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			event, found := scripts.NativePuppetChoiceAt(point, jonesActiveChoices)
			if !found {
				return currentFrame, false, nil
			}
			jonesChoicePressActive, jonesChoicePressEvent = true, event
			jonesChoicePressIndex = (int(int16(point)) - 264) / 24
			return currentFrame, false, nil
		}
		if isaoInteractionStage == isaoInteractionPuppetChoices {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			event, found := scripts.NativePuppetChoiceAt(point, isaoActiveChoices)
			if !found {
				return currentFrame, false, nil
			}
			isaoChoicePressActive, isaoChoicePressEvent = true, event
			isaoChoicePressIndex = (int(int16(point)) - 264) / 24
			return currentFrame, false, nil
		}
		if isaoInteractionStage == isaoInteractionInventory && currentScene == 2 && mouseEvent.Button == ebiten.MouseButtonLeft {
			if name, found := render.HitTestFlatProps(marieInventoryProjected, image.Pt(int(int16(point>>16)), int(int16(point)))); found {
				handItem = name
				if err := renderMarieInventory(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				if *debug {
					log.Printf("inventory-select item=%s owner=%s flat=avatar recipient=isao", handItem, inventoryOwners[strings.ToLower(handItem)])
				}
				return currentFrame, true, nil
			}
		}
		if isaoInteractionStage != isaoInteractionIdle && isaoInteractionStage != isaoInteractionInventory {
			return currentFrame, false, nil
		}
		if jonesInteractionStage != jonesInteractionIdle {
			return currentFrame, false, nil
		}
		if marieInteractionStage == marieInteractionInventory && currentScene == 2 && mouseEvent.Button == ebiten.MouseButtonLeft {
			if name, found := render.HitTestFlatProps(marieInventoryProjected, image.Pt(int(int16(point>>16)), int(int16(point)))); found {
				handItem = name
				if err := renderMarieInventory(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				if *debug {
					log.Printf("inventory-select item=%s owner=%s flat=avatar", handItem, inventoryOwners[strings.ToLower(handItem)])
				}
				return currentFrame, true, nil
			}
		}
		if marieInteractionStage == marieInteractionPuppetChoices {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			event, found := scripts.NativePuppetChoiceAt(point, marieActiveChoices)
			if !found {
				return currentFrame, false, nil
			}
			marieChoicePressActive, marieChoicePressEvent = true, event
			marieChoicePressIndex = (int(int16(point)) - 264) / 24
			return currentFrame, false, nil
		}
		if marieInteractionStage != marieInteractionIdle && marieInteractionStage != marieInteractionInventory {
			return currentFrame, false, nil
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
			if boneInInventory && strings.EqualFold(handItem, "Bone") && !boneDragging && mouseEvent.Button == ebiten.MouseButtonLeft {
				mousePoint := image.Pt(int(int16(point>>16)), int(int16(point)))
				hitBone, err := render.HitTestPuppetFrame(boneInventoryFrame, image.Pt(316, 320), mousePoint)
				if err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("hit test Bone inventory frame: %w", err)
				}
				if hitBone {
					boneDragging, boneDragLast = true, mousePoint
					if err := refreshWorldScene(); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("start Bone drag: %w", err)
					}
					dragFrame, err := render.CompositePuppetFrame(currentFrame, boneInventoryFrame, mousePoint)
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					currentFrame, stageFrame = dragFrame, dragFrame
					return currentFrame, true, nil
				}
			}
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
			if mouseEvent.Button == ebiten.MouseButtonLeft && activeSetName == "town" {
				if owner, found := scripts.NiteDoorAt(view.Resource, worldPoint[2], point); found {
					locked, err := scripts.NiteDoorLocked(owner, gameDay, gameClock, gamePhase, false, false, &nativeRandom)
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					doorOwner = ""
					if locked {
						if err := soundBank.Play(audioContext, "knock1", 1); err != nil {
							return render.IndexedFrame{}, false, fmt.Errorf("play locked-door knock: %w", err)
						}
						if *debug {
							log.Printf("door=%s locked=true view=%s day=%d clock=%d phase=%d", owner, view.Name[1:], gameDay, gameClock, gamePhase)
						}
					} else {
						doorOwner = owner
						if *debug {
							log.Printf("door=%s owner=door view=%s direction=%d", owner, view.Name[1:], worldPoint[2])
						}
					}
					return currentFrame, false, nil
				}
			} else if mouseEvent.Button == ebiten.MouseButtonLeft && activeSetName == "sallower" {
				if owner, found := scripts.SallowerDoorAt(view.Resource, worldPoint[2], point); found {
					doorOwner = owner
					if *debug {
						log.Printf("door=%s owner=door set=sallower direction=%d", owner, worldPoint[2])
					}
					return currentFrame, false, nil
				}
			} else if mouseEvent.Button == ebiten.MouseButtonLeft && activeSetName == "hotlower" {
				if owner, found := scripts.HotLowerDoorAt(view.Resource, worldPoint[2], point); found {
					doorOwner = owner
					if *debug {
						log.Printf("door=%s owner=door set=hotlower direction=%d", owner, worldPoint[2])
					}
					return currentFrame, false, nil
				}
			}
			if actorName, found := render.HitTestWorldActors(projectedActors, image.Pt(int(int16(point>>16)), int(int16(point)))); found {
				if *debug {
					log.Printf("world-actor-hit=%s", actorName)
				}
				if strings.EqualFold(actorName, "Isao") {
					started, err := beginIsaoPuppetTalk()
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, started, nil
				}
				if strings.EqualFold(actorName, "Bone") && boneWorldProp.Visible && boneOwner == "none" {
					camera := render.NativeActorCameraPosition(worldPoint)
					playerPosition := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
					distance := scripts.NativeActorDistance2D(boneWorldProp.Position, playerPosition)
					if distance < 512 {
						boneInventoryFrame = inventoryLargeFrames["bone"]
						boneInInventory, boneOwner, inventoryOwners["bone"], handItem, boneWorldProp.View, boneWorldProp.Visible = true, "stranger", "stranger", "Bone", boneLargeView, false
						if err := soundBank.Play(audioContext, "inven", 1); err != nil {
							return render.IndexedFrame{}, false, fmt.Errorf("play Bone inventory sound: %w", err)
						}
						if *debug {
							log.Printf("prop=Bone addinven owner=%s view=large distance=%d panel=316,320", boneOwner, distance)
						}
						if err := refreshWorldScene(); err != nil {
							return render.IndexedFrame{}, false, fmt.Errorf("refresh after Bone pickup: %w", err)
						}
						return currentFrame, true, nil
					}
					return currentFrame, false, nil
				}
				if strings.EqualFold(actorName, "Help") {
					started, err := beginHelpPuppetTalk()
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, started, nil
				}
				if strings.EqualFold(actorName, "dog") && activeSetName == "town" {
					camera := render.NativeActorCameraPosition(worldPoint)
					player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
					dogDistance := 1 << 30
					for _, actor := range worldActors {
						if strings.EqualFold(actor.Name, "dog") {
							dogDistance = scripts.NativeActorDistance2D(actor.Position, player)
							break
						}
					}
					sceneName, handlesClick := scripts.CastActorMouseDownScene(actorName, gameDay, dogDistance, townActorHotDistance)
					if !handlesClick {
						if *debug {
							log.Printf("actor=dog mousedown=ignored day=%d distance=%d hotdist=%d", gameDay, dogDistance, townActorHotDistance)
						}
						return currentFrame, false, nil
					}
					nextFrame, err := setWorldView(sceneName, worldPoint[2])
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					if name, triggered := scripts.NiteDogGateMovieInView(view.Resource, worldPoint[2], gameDay, dogVisibleState); triggered {
						dogMovieNeedsHelp = true
						if err := startSceneMovie(name); err != nil {
							return render.IndexedFrame{}, false, err
						}
						if *debug {
							log.Printf("actor=dog mousedown=G12-keydown-up movie=%s", name)
						}
						frame, err := movieOutputFrame(playback.CurrentFrame())
						if err != nil {
							return render.IndexedFrame{}, false, err
						}
						return frame, true, nil
					}
					return nextFrame, true, nil
				}
				if strings.EqualFold(actorName, "Leroy") && strings.EqualFold(string(view.Name[1:]), "Scene G15") && leroyInteractionStage == leroyInteractionIdle {
					playerWorld := render.NativeActorCameraPosition(worldPoint)
					playerPosition := [3]int16{int16(playerWorld[0]), int16(playerWorld[1]), int16(playerWorld[2])}
					distance := scripts.NativeActorDistance2D(leroyPosition, playerPosition)
					if scripts.LeroyMouseDownAction(gameDay, distance, townActorHotDistance) {
						if !scripts.NativeWalktopuppetAxisAligned(leroyPosition, playerPosition) {
							if *debug {
								log.Printf("actor=leroy walktopuppet=blocked-axis-alignment actor=%v player=%v", leroyPosition, playerPosition)
							}
							return currentFrame, true, nil
						}
						destination := [3]int16{playerPosition[0], playerPosition[1], 0}
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
				if strings.EqualFold(actorName, "Jones") && jonesInteractionStage == jonesInteractionIdle {
					started, err := beginJonesPuppetTalk()
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, started, nil
				}
				if strings.EqualFold(actorName, "Marie") && marieInteractionStage == marieInteractionIdle {
					started, err := beginMariePuppetTalk()
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, started, nil
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
			worldBackground, visibleActors, actorErr := compositeWorld(backgroundFrame, worldPoint, worldActors)
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
		if isaoInteractionStage == isaoInteractionInventory && target == 0 {
			isaoInventoryReturnPending, isaoInteractionStage = true, isaoInteractionInventoryReturning
		}
		if marieInteractionStage == marieInteractionInventory && target == 0 {
			marieInventoryReturnPending, marieInteractionStage = true, marieInteractionInventoryReturning
		}
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
			case scripts.LookupOpcode("plain"):
				effectName = "plain"
				if action.Duration > 0 {
					transition, err = render.NewFadeEffect(currentFrame, nextFrame, action.Duration)
				} else {
					currentFrame = nextFrame
				}
			default:
				return render.IndexedFrame{}, false, fmt.Errorf("button script resource %d uses unsupported visual effect %d", handler.ScriptResource, action.VisualEffect)
			}
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("start %s transition: %w", effectName, err)
			}
			if effectName != "plain" {
				if err := soundBank.Play(audioContext, "pageturn", 4); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("play page-turn sound: %w", err)
				}
			}
			if *debug {
				log.Printf("visualeffect=%s duration=%d", effectName, action.Duration)
				if effectName != "plain" {
					log.Printf("sound=pageturn active-players=%d", soundBank.ActivePlayers())
				}
			}
			if transition != nil {
				if effectName == "plain" || marieInventoryReturnPending || isaoInventoryReturnPending {
					transitionMode = 2
				}
				return transition.CurrentFrame(), true, nil
			}
		}
		currentFrame = nextFrame
		if isaoInventoryReturnPending {
			isaoInventoryReturnPending = false
			if err := resumeIsaoInventory(); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		}
		if marieInventoryReturnPending {
			marieInventoryReturnPending = false
			if err := resumeMarieInventory(); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		}
		if *debug {
			log.Printf("scene transition=%s resource=%d", stage.Scenes[target].Name[1:], stage.Scenes[target].Fields[1])
		}
		return nextFrame, true, nil
	}, func(state engine.MouseState) (render.IndexedFrame, bool, error) {
		updateNativeCursor(state.Point)
		if helpInteractionStage == helpInteractionPuppetChoices && helpChoicePressActive {
			event, found := scripts.NativePuppetChoiceAt(state.Point, helpActiveChoices)
			outline := -1
			if found && event == helpChoicePressEvent {
				outline = (int(int16(state.Point)) - 264) / 24
			}
			if state.LeftDown {
				if outline == helpChoiceOutline {
					return currentFrame, false, nil
				}
				if err := drawHelpChoices(outline); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				selected := outline >= 0 && outline == helpChoicePressIndex
				selectedEvent := helpChoicePressEvent
				helpChoicePressActive, helpChoicePressIndex = false, -1
				if selected {
					if err := startHelpChoice(selectedEvent); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("run Help choice %d: %w", selectedEvent, err)
					}
					return currentFrame, true, nil
				}
				if helpChoiceOutline >= 0 {
					if err := drawHelpChoices(-1); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				return currentFrame, false, nil
			}
			return currentFrame, false, nil
		}
		if isaoInteractionStage == isaoInteractionPuppetChoices && isaoChoicePressActive {
			event, found := scripts.NativePuppetChoiceAt(state.Point, isaoActiveChoices)
			outline := -1
			if found && event == isaoChoicePressEvent {
				outline = (int(int16(state.Point)) - 264) / 24
			}
			if state.LeftDown {
				if outline == isaoChoiceOutline {
					return currentFrame, false, nil
				}
				if err := drawIsaoChoices(outline); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				selected := outline >= 0 && outline == isaoChoicePressIndex
				selectedEvent := isaoChoicePressEvent
				isaoChoicePressActive, isaoChoicePressIndex = false, -1
				if selected {
					if err := startIsaoEventResponse(selectedEvent); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("run Isao choice %d: %w", selectedEvent, err)
					}
					return currentFrame, true, nil
				}
				if isaoChoiceOutline >= 0 {
					if err := drawIsaoChoices(-1); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				return currentFrame, false, nil
			}
			return currentFrame, false, nil
		}
		if jonesInteractionStage == jonesInteractionPuppetChoices && jonesChoicePressActive {
			event, found := scripts.NativePuppetChoiceAt(state.Point, jonesActiveChoices)
			outline := -1
			if found && event == jonesChoicePressEvent {
				outline = (int(int16(state.Point)) - 264) / 24
			}
			if state.LeftDown {
				if outline == jonesChoiceOutline {
					return currentFrame, false, nil
				}
				if err := drawJonesChoices(outline); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				selected := outline >= 0 && outline == jonesChoicePressIndex
				selectedEvent := jonesChoicePressEvent
				jonesChoicePressActive, jonesChoicePressIndex = false, -1
				if selected {
					if err := startJonesEventResponse(selectedEvent); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("run Jones choice %d: %w", selectedEvent, err)
					}
					return currentFrame, true, nil
				}
				if jonesChoiceOutline >= 0 {
					if err := drawJonesChoices(-1); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				return currentFrame, false, nil
			}
			return currentFrame, false, nil
		}
		if marieInteractionStage == marieInteractionPuppetChoices && marieChoicePressActive {
			event, found := scripts.NativePuppetChoiceAt(state.Point, marieActiveChoices)
			outline := -1
			if found && event == marieChoicePressEvent {
				outline = (int(int16(state.Point)) - 264) / 24
			}
			if state.LeftDown {
				if outline == marieChoiceOutline {
					return currentFrame, false, nil
				}
				if err := drawMarieChoices(outline); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				selected := outline >= 0 && outline == marieChoicePressIndex
				selectedEvent := marieChoicePressEvent
				marieChoicePressActive, marieChoicePressIndex = false, -1
				if selected {
					if err := startMarieEventResponse(selectedEvent); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("run Marie choice %d: %w", selectedEvent, err)
					}
					return currentFrame, true, nil
				}
				if marieChoiceOutline >= 0 {
					if err := drawMarieChoices(-1); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				return currentFrame, false, nil
			}
			return currentFrame, false, nil
		}
		if boneDragging {
			point := image.Pt(int(int16(state.Point>>16)), int(int16(state.Point)))
			if state.LeftDown {
				if point == boneDragLast {
					return currentFrame, false, nil
				}
				boneDragLast = point
				if err := refreshWorldScene(); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("update Bone drag background: %w", err)
				}
				dragFrame, err := render.CompositePuppetFrame(currentFrame, boneInventoryFrame, point)
				if err != nil {
					return render.IndexedFrame{}, false, err
				}
				currentFrame, stageFrame = dragFrame, dragFrame
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				boneDragging = false
				actorName, hit := render.HitTestWorldActors(projectedActors, point)
				if hit && strings.EqualFold(actorName, "dog") && boneInInventory && boneOwner == "stranger" && gameDay != 5 {
					boneInInventory, boneOwner, boneWorldProp.Visible = false, "none", false
					inventoryOwners["bone"], handItem = "none", ""
					dogVisibleState, dog2OfferStage = false, dog2OfferMovie
					nativeLoops.Stop(2, "dog")
					if _, err := setWorldView("Scene G12", assets.SetDirectionNorth); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("prepare Dog offer scene: %w", err)
					}
					if err := startSceneMovie("MOVIES/DOG2.MOV"); err != nil {
						return render.IndexedFrame{}, false, err
					}
					if *debug {
						log.Printf("actor=dog offerobject=Bone visible=false phase=%d movie=DOG2.MOV", gamePhase)
					}
					frame, err := movieOutputFrame(playback.CurrentFrame())
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					return frame, true, nil
				}
				if err := refreshWorldScene(); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("finish Bone drag: %w", err)
				}
				return currentFrame, true, nil
			}
			return currentFrame, false, nil
		}
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
