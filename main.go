package main

import (
	"encoding/binary"
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
	"redust/save"
	"redust/scripts"
)

func loadInventoryFrame(archive *assets.PropArchive, propName, viewName string, angle int16) (render.PuppetFrame, error) {
	return loadPropFrame(archive, propName, viewName, 0, angle)
}

func loadPropFrame(archive *assets.PropArchive, propName, viewName string, frameIndex int, angle int16) (render.PuppetFrame, error) {
	info, err := archive.FrameInfo(propName, viewName, frameIndex, angle)
	if err != nil {
		return render.PuppetFrame{}, err
	}
	data, err := archive.Resource(info.Resource)
	if err != nil {
		return render.PuppetFrame{}, err
	}
	return render.DecodePuppetFrame(data)
}

func nativeDogSpotMovie(name string) (string, bool) {
	name = strings.ToUpper(filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, `/`)))
	if name != "DOG1.MOV" && name != "DOG2.MOV" {
		return "", false
	}
	return "MOVIES/" + name, true
}

func nativeSpotMovieReturnSet(clock int) string {
	setName := "town.set"
	if clock == 3 {
		setName = "nite.set"
	}
	return setName
}

func nativeMovieHasEmbeddedAudio(movie *render.Movie) bool {
	if movie == nil {
		return false
	}
	resources, _ := movie.SoundtrackResources()
	return len(resources) == 0 && len(movie.EmbeddedSoundResources()) > 0
}

func startNativeMovieAudio(context *ebitenaudio.Context, movie *render.Movie) (*audio.Player, int, int, error) {
	if movie == nil {
		return nil, 0, 0, fmt.Errorf("movie is unavailable")
	}
	resources, loopIndex := movie.SoundtrackResources()
	if len(resources) == 0 {
		for index := 0; index < movie.FrameCount(); index++ {
			frame, err := movie.Frame(index)
			if err != nil {
				return nil, 0, 0, err
			}
			if len(frame.Events) > 0 {
				return nil, 0, loopIndex, nil
			}
		}
		resources = movie.EmbeddedSoundResources()
		if len(resources) == 0 || context == nil {
			return nil, 0, loopIndex, nil
		}
		loopIndex = -1
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
	player, err := audio.NewNativePlaylist(context, tracks, events, loopIndex)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("start movie soundtrack: %w", err)
	}
	return player, len(events), loopIndex, nil
}

const scoreMenuVolumeTop = 225
const scoreMenuVolumeBottom = 323

func scoreMenuVolumeAt(point uint32, track image.Rectangle) (int, bool) {
	x, y := int(int16(point>>16)), int(int16(point))
	if x < track.Min.X || x >= track.Max.X || y < track.Min.Y || y >= track.Max.Y {
		return 0, false
	}
	return ((scoreMenuVolumeBottom-y)*9 + (scoreMenuVolumeBottom-scoreMenuVolumeTop)/2) / (scoreMenuVolumeBottom - scoreMenuVolumeTop), true
}

func loadInventoryScenePixels(stage *assets.Stage, palette []byte) (render.MoviePixels, error) {
	lease, err := stage.AcquireSceneFrameResource(2)
	if err != nil {
		return render.MoviePixels{}, err
	}
	data, err := lease.Bytes()
	if closeErr := lease.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return render.MoviePixels{}, err
	}
	pixels, decodeErr := render.DecodeMoviePixels(data, palette)
	if decodeErr != nil && len(pixels.Pixels) == 0 {
		return render.MoviePixels{}, fmt.Errorf("decode avatar inventory flat: %w", decodeErr)
	}
	return pixels, nil
}

func inventoryCashNeedsRefresh(rendered int32, valid bool, current int32) bool {
	return !valid || rendered != current
}

func run() error {
	work := flag.String("work", "bin", "work directory; assets are read from its assets child")
	debug := flag.Bool("debug", false, "enable diagnostics")
	debugLoops := flag.Bool("debug-loops", false, "log recurring actor idle and ambient callbacks")
	silent := flag.Bool("silent", false, "render the initial level to a PNG and exit without opening a window")
	silentScript := flag.String("silent-script", "", "run a JSON input script without opening a window or playing audio")
	silentSeed := flag.Uint("silent-seed", 0, "native random seed for scripted headless checks; zero uses the clock")
	loadPath := flag.String("load", "", "load a ReDust save before starting")
	flag.Parse()
	if *silentScript != "" {
		*silent = true
	}
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
	var themePlayer *audio.Player
	var audioContext *ebitenaudio.Context
	var nativeLoops scripts.LoopScheduler
	var nativeRandom scripts.NativeRandom
	resetNativeRandom := func() {
		seed := scripts.NativeRandomSeed(scripts.NativeTickMilliseconds())
		if *silentScript != "" && *silentSeed != 0 {
			seed = uint32(*silentSeed)
		}
		nativeRandom = scripts.NewNativeRandom(seed)
	}
	resetNativeRandom()
	setInventoryLoopsPaused := func(paused bool) {
		for kind := uint16(1); kind <= 4; kind++ {
			nativeLoops.SetPaused(kind, "all", paused)
		}
	}
	var currentThemeName string
	var currentTheme audio.NativeTheme
	var pendingSpotMovie, spotMovieName string
	var spotMovieReturnFrame render.IndexedFrame
	var spotMovieActionFrameOne, spotMovieActive, spotMovieStarting bool
	var sceneMovieAfter func(bool) (render.IndexedFrame, bool, error)
	if !*silent || *silentScript != "" {
		if *silent {
			defer audio.SetOutputMuted(audio.SetOutputMuted(true))
		}
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
	mainStage := stage
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
	playercash := int32(5)
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
	avatarViewName := "gossip"
	avatarFrameIndex, avatarAngle := 0, int16(0)
	avatarFrameCache := map[string]render.PuppetFrame{}
	var avatarTipActive bool
	var avatarTipFrame int
	var avatarTipNextFrame uint32
	var avatarTipAfter func() (bool, error)
	var avatarIdleActive, avatarMakefaceDue bool
	var avatarIdleNext uint32
	const nativeAvatarFrameInterval uint32 = 3
	var avatarGestureActive bool
	var avatarGestureNext uint32
	startAvatarNoFace := func(now uint32) {
		if gameClock == 3 {
			avatarViewName = "nitefaces"
		} else {
			avatarViewName = "dayfaces"
		}
		avatarFrameIndex, avatarAngle = 0, 0
		avatarIdleActive, avatarMakefaceDue = true, true
		avatarGestureActive = false
		avatarIdleNext = now + nativeAvatarFrameInterval*uint32(nativeRandom.Inclusive(30)+30)
	}
	avatarFrameForCurrent := func() (render.PuppetFrame, image.Point, error) {
		if avatarViewName == "gossip" {
			degree := 1
			if gameClock == 3 {
				degree = 0
			}
			portrait := avatarFrames[degree]
			portrait.Origin = image.Pt(portrait.Origin.Y, portrait.Origin.X)
			return portrait, image.Pt(460, 325), nil
		}
		key := fmt.Sprintf("%s:%d:%d", avatarViewName, avatarFrameIndex, avatarAngle)
		portrait, found := avatarFrameCache[key]
		if !found {
			var err error
			portrait, err = loadPropFrame(propArchive, "avatar", avatarViewName, avatarFrameIndex, avatarAngle)
			if err != nil {
				return render.PuppetFrame{}, image.Point{}, err
			}
			avatarFrameCache[key] = portrait
		}
		portrait.Origin = image.Pt(portrait.Origin.Y, portrait.Origin.X)
		return portrait, image.Pt(460, 325), nil
	}
	if *debug {
		log.Printf("prop=avatar view=gossip degree-resources=%d,%d anchor=460,325 source=NEW.FLT/noface", avatarResources[0], avatarResources[1])
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
	actorPoses := map[string]string{"leroy": "stand", "dog": "stand", "help": "stand", "jones": "stand", "buick": "stand", "marie": "stand", "isao": "stand", "trotter": "stand", "laurel": "stand"}
	actorPoses["laurel"] = "stand"
	actorHeadings := map[string]int16{"leroy": 0, "dog": 32, "help": 0, "jones": 0, "buick": 0, "marie": 128, "isao": 64, "trotter": 0, "laurel": 0}
	actorTurnTargets := map[string]int16{"leroy": 0, "help": 0, "jones": 0, "buick": 0, "marie": 128, "isao": 64}
	actorTurnActive, helpTurnActive, jonesTurnActive := false, false, false
	marieVisible := gameDay == 2 && gameClock == 3
	marieTurnActive := false
	marieStar := "town.jones2"
	helpVisible, helpPhase, helpAttention := false, int16(0), int32(0)
	jonesPosition, jonesVisible := jonesStartPosition, false
	var isaoPosition [3]int16
	var trotterPosition [3]int16
	isaoVisible, isaoBouncer, isaoDirGo := false, false, false
	trotterVisible := false
	// Script-managed actors are driven by the general interpreter over their
	// shipped scripts instead of hand-written state. The table holds every cast
	// actor, built by the native constructor (FUN_0040C1F0); only the managed
	// ones are drawn from it and have their loops dispatched through it, while
	// the rest keep their existing hand-coded paths for now.
	scriptActors := scripts.NewScriptActors()
	scriptActorCasts := map[string]assets.Cast{}
	scriptActorRecords := map[string]assets.CastActor{}
	scriptManaged := map[string]bool{"mwife": true, "blood": true, "buick": true}
	for _, cast := range []assets.Cast{gangCast, extraCast} {
		for _, actor := range cast.Actors {
			key := strings.ToLower(actor.Name)
			if _, exists := scriptActorRecords[key]; exists {
				continue
			}
			poses := make([]string, len(actor.Poses))
			for index, pose := range actor.Poses {
				poses[index] = pose.Name
			}
			scriptActors.Add(scripts.NewCastActorRecord(cast.Name, actor.Name, actor.Selector, actor.Location, actor.Script, poses))
			scriptActorCasts[key], scriptActorRecords[key] = cast, actor
		}
	}
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
	const (
		trotterInteractionIdle uint8 = iota
		trotterInteractionPuppetSpeaking
		trotterInteractionPuppetChoices
		trotterInteractionInventory
		trotterInteractionInventoryReturning
		trotterInteractionPuppetDelay
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

	// leroyStandingSpeech is the line list the current conversation speaks, kept so the choice
	// screen can build the puppet's standing pose from the conversation's *own* first line
	// rather than from whichever line the table happens to list first.
	var leroyStandingSpeech []string
	// A choice base used to be captured from `currentFrame` on entry, so the choice screen
	// inherited whatever was last on screen. That is what put an animated puppet's own pixels
	// under the standing pose and made the figure appear twice, so **there is no choice base
	// any more**: each choice screen builds its own from the conversation base, which is scene
	// only, plus the standing pose it draws itself.
	helpInteractionStage := helpInteractionIdle
	jonesInteractionStage := jonesInteractionIdle
	jonesPhase, laurelPhase := int16(0), int16(0)
	jonesRingStory := int16(0)
	jonesActorValue := int32(0)
	laurelGood, hankerchiefDegree := int16(0), int16(0)
	laurelActorValue, laurelCounter, oonakidstory := int32(0), int32(0), int32(0)
	var laurelVisible bool
	var laurelPosition [3]int16
	var laurelWalk *scripts.NativeActorWalkJob
	var laurelWalkNext uint32
	var laurelPuppet *render.Puppet
	var laurelSpeechTable assets.PuppetSpeechTable
	var laurelController *scripts.LaurelScriptController
	var laurelDialogue *engine.PuppetDialogue
	var laurelConversationBase render.IndexedFrame
	var laurelChoices []scripts.PuppetChoice
	var laurelContinuation *scripts.LaurelContinuation
	var laurelChoicePressActive bool
	var laurelChoicePressEvent int32
	laurelChoicePressIndex, laurelChoiceOutline := -1, -1
	var laurelDialogueSkip bool
	var laurelChoiceUntil uint32
	var laurelPendingStep scripts.LaurelStep
	var laurelFrameIndex int
	var laurelInventoryReturnPending bool
	var openLaurelInventory func() error
	const (
		laurelInteractionIdle uint8 = iota
		laurelInteractionSpeaking
		laurelInteractionChoices
		laurelInteractionInventory
		laurelInteractionInventoryReturning
	)
	laurelInteractionStage := laurelInteractionIdle
	isaoInteractionStage := isaoInteractionIdle
	isaoPhase, trotterPhase, isaoActorValue, oonaActorValue := int16(0), int16(0), int32(0), int32(0)
	trotterInteractionStage := trotterInteractionIdle
	var trotterPuppet *render.Puppet
	var trotterPuppetTable assets.PuppetSpeechTable
	var trotterScriptController *scripts.TrotterScriptController
	var trotterDialogue *engine.PuppetDialogue
	var trotterConversationBase render.IndexedFrame
	var trotterDialogueSkip bool
	var trotterPendingStep scripts.TrotterStep
	var trotterGiftCounter int32
	var trotterActorValue int32
	var bloodPhase, docPhase, fightOn int16
	var fearPhase int16
	hotelSavedScene, hotelSavedDirection := "Scene C4", "east"
	var hotelSceneOpenAction *scripts.HotelSceneOpenAction
	hotelSceneOpenStep, hotelEventsLocked := 0, false
	var pendingHotelActorSetup, pendingHotelActorSelector string
	var hotelJonesVisible bool
	var hotelJonesPosition [3]int16
	var hotelJonesWalk *scripts.NativeActorWalkJob
	var hotelVoiceSteps []scripts.HotelVoiceStep
	var hotelVoiceIndex int
	var hotelVoice *engine.VoiceOne
	var hotelVoiceBank *audio.SoundBank
	var hotelDelayUntil uint32
	var hotelDoorAfterDelay string
	var hotelHotplateStage *assets.Stage
	var hotelHotplateBank *audio.SoundBank
	var hotelHotplatePending scripts.HotelAction
	var applyFlatTarget func(int, uint16, int, uint32) (render.IndexedFrame, bool, error)
	defer func() {
		if hotelVoice != nil {
			_ = hotelVoice.Close()
		}
		if hotelVoiceBank != nil {
			_ = hotelVoiceBank.Close()
		}
		if hotelHotplateBank != nil {
			_ = hotelHotplateBank.Close()
		}
		if hotelHotplateStage != nil {
			_ = hotelHotplateStage.Close()
		}
	}()
	var trotterSpeechStages []scripts.TrotterSpeechStage
	var trotterSpeechStageIndex int
	var trotterDelayUntil, trotterChoiceUntil uint32
	var trotterWalk *scripts.NativeActorWalkJob
	var trotterWalkNext uint32
	var trotterFrameIndex int
	var trotterTurnSpeed int16
	trotterActorSet, trotterStar := "sallower", "sal.trotter1"
	trotterScale, trotterSpeed, trotterTurnSpeed, trotterZClip := int16(4500), int16(4), int16(8), int16(32)
	var trotterActiveChoices []scripts.PuppetChoice
	var trotterChoicePressActive bool
	var trotterChoicePressEvent int32
	trotterChoicePressIndex, trotterChoiceOutline := -1, -1
	var trotterInventoryReturnPending bool
	var openTrotterInventory func() error
	var finishPlayerDeath func(string) error
	var deathSequence scripts.DeathSequence
	var deathStage uint8
	var deathUntil uint32
	var deathFrame render.IndexedFrame
	var deathBank *audio.SoundBank
	var deathNarration *audio.Player
	defer func() {
		if deathNarration != nil {
			_ = deathNarration.Close()
		}
		if deathBank != nil {
			_ = deathBank.Close()
		}
	}()
	var isaoPuppet *render.Puppet
	var isaoPuppetTable assets.PuppetSpeechTable
	var isaoDialogue *engine.PuppetDialogue
	var isaoConversationBase render.IndexedFrame
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
	var isaoKeyMoviePending, isaoKeyMovieActive, isaoThemeResume bool
	var isaoInventoryReturnPending bool
	var isaoInventoryReturnCode string
	var isaoGiftCounter int32
	helpActorPosition, helpReturnPosition := helpPosition, helpPosition
	var helpWalk *scripts.NativeActorWalkJob
	var jonesWalk *scripts.NativeActorWalkJob
	var marieWalk *scripts.NativeActorWalkJob
	var marieWalkTarget string
	var jonesPuppet *render.Puppet
	var jonesPuppetTable assets.PuppetSpeechTable
	var jonesPuppetProgram scripts.Program
	var jonesDialogue *engine.PuppetDialogue
	var jonesConversationBase render.IndexedFrame
	var jonesChoiceGroups [][]scripts.PuppetChoice
	var jonesActiveChoices []scripts.PuppetChoice
	var jonesChoiceGroup int
	var jonesDialogueSkip bool
	var jonesChoicePressActive bool
	var jonesChoicePressEvent int32
	jonesChoicePressIndex, jonesChoiceOutline := -1, -1
	jonesNextChoiceGroup := -1
	var jonesSetPhaseOnFinish bool
	jonesPuppetCode := "threenite"
	marieInteractionStage := marieInteractionIdle
	mariePhase, marieFlag1, marieFlag2, marieFlag3 := int16(0), false, false, false
	marieActorValue := int32(0)
	handItem, handFlag, inventoryMenuActive := "", int16(0), false
	var marieGiftCounter int32
	var mariePuppet *render.Puppet
	var mariePuppetTable assets.PuppetSpeechTable
	var mariePuppetProgram, marieInventoryProgram scripts.Program
	var marieHandBevelChoices []scripts.PuppetChoice
	var marieDialogue *engine.PuppetDialogue
	var marieConversationBase render.IndexedFrame
	var marieChoiceGroups [][]scripts.PuppetChoice
	var marieActiveChoices []scripts.PuppetChoice
	var marieInventoryProjected []render.ProjectedFlatProp
	var inventoryCashRendered int32
	var inventoryCashRenderedValid bool
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
	var helpConversationBase render.IndexedFrame
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
	inventoryOwners := map[string]string{"gun": "none", "boots": "none", "bullets": "none", "badge": "none", "hankerchief": "none", "hhkey": "none", "hairpin": "none", "ring": "none", "bone": "none"}
	inventoryHidden := make(map[string]bool)
	inventoryDegrees := make(map[string]int16)
	var boneDragging bool
	var boneDragLast image.Point
	var dog2PhaseAfterHelp bool
	defer func() {
		if trotterDialogue != nil {
			_ = trotterDialogue.Close()
		}
		if trotterPuppet != nil {
			_ = trotterPuppet.Close()
		}
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
		return render.CompositeWorldActorsAndProps(background, point, activeSetName, actors, []render.WorldPropSprite{boneWorldProp}, render.WorldOccludersForView(activeSetName, point))
	}
	loadWorldActors := func(point [3]int16) ([]render.WorldActorSprite, error) {
		actors := make([]render.WorldActorSprite, 0, 4)
		for _, key := range scriptActors.Names() {
			record, _ := scriptActors.Lookup(key)
			if !scriptManaged[key] || !record.Visible || !strings.EqualFold(record.Set, activeSetName) {
				continue
			}
			castActor := scriptActorRecords[key]
			castActor.Position, castActor.Located = record.Position, true
			if *debug && *debugLoops {
				log.Printf("script-actor-draw %s", record)
			}
			sprite, err := render.LoadCastActorFrame(workspace, scriptActorCasts[key], castActor, record.Pose, int(record.Frame), int16(record.Scale), render.NativeActorViewAngle(record.Position, point, record.Heading), record.ZClip)
			if err != nil {
				return nil, fmt.Errorf("load script actor %s in %s: %w", record.Name, activeSetName, err)
			}
			actors = append(actors, sprite)
		}
		if trotterVisible && strings.EqualFold(trotterActorSet, activeSetName) {
			for _, actor := range gangCast.Actors {
				if !strings.EqualFold(actor.Name, "Trotter") {
					continue
				}
				actor.Position, actor.Located = trotterPosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["trotter"], trotterFrameIndex, trotterScale, render.NativeActorViewAngle(trotterPosition, point, actorHeadings["trotter"]), trotterZClip)
				if err != nil {
					return nil, fmt.Errorf("load Trotter actor in %s: %w", activeSetName, err)
				}
				actors = append(actors, sprite)
				break
			}
		}
		if laurelVisible && activeSetName == "hotlower" {
			for _, actor := range gangCast.Actors {
				if !strings.EqualFold(actor.Name, "Laurel") {
					continue
				}
				actor.Position, actor.Located = laurelPosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["laurel"], laurelFrameIndex, 3220, render.NativeActorViewAngle(laurelPosition, point, actorHeadings["laurel"]), 32)
				if err != nil {
					return nil, fmt.Errorf("load HOTLOWER actor Laurel: %w", err)
				}
				actors = append(actors, sprite)
				break
			}
		}
		// No cell cull is applied here. Two attempts were made and both were wrong: the
		// first required the actor to be in the cell exactly one step ahead, which hid
		// every actor in the street until the player walked into their cell, and the second
		// admitted anything ahead in the facing direction, which did not hold up either.
		// The reference's own rule has not been read out of the binary yet, and guessing it
		// cost a regression. See assets.ActorCellVisible, which is kept as the measured
		// cell arithmetic but is deliberately not wired in.
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
		if activeSetName == "hotupper" && hotelJonesVisible {
			for _, actor := range gangCast.Actors {
				if !strings.EqualFold(actor.Name, "Jones") {
					continue
				}
				actor.Position, actor.Located = hotelJonesPosition, true
				sprite, err := render.LoadCastActorFrame(workspace, gangCast, actor, actorPoses["jones"], 0, 1450, render.NativeActorViewAngle(hotelJonesPosition, point, actorHeadings["jones"]), 32)
				if err != nil {
					return nil, fmt.Errorf("load HOTUPPER actor Jones: %w", err)
				}
				actors = append(actors, sprite)
				break
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
			sprite, err := render.LoadCastActorFrame(workspace, extraCast, actor, actorPoses["dog"], 0, 880, render.NativeActorViewAngle(actor.Position, point, actorHeadings["dog"]), 64)
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
	scriptCaches := map[string]*assets.ResourceCache{}
	scriptPrograms := map[string]*scripts.Program{}
	scriptCache := func(castName string) (*assets.ResourceCache, error) {
		if cache, found := scriptCaches[castName]; found {
			return cache, nil
		}
		cache, err := workspace.OpenResourceCache(castName)
		if err != nil {
			return nil, err
		}
		scriptCaches[castName] = cache
		return cache, nil
	}
	scriptResource := func(castName string, resource uint32) ([]byte, error) {
		cache, err := scriptCache(castName)
		if err != nil {
			return nil, err
		}
		lease, err := cache.Acquire(resource)
		if err != nil {
			return nil, err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		return append([]byte(nil), data...), err
	}
	scriptProgram := func(castName string, resource uint32) (*scripts.Program, error) {
		key := fmt.Sprintf("%s#%d", castName, resource)
		if program, found := scriptPrograms[key]; found {
			return program, nil
		}
		data, err := scriptResource(castName, resource)
		if err != nil {
			return nil, err
		}
		program, err := scripts.ParseProgram(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s resource %d: %w", castName, resource, err)
		}
		scriptPrograms[key] = &program
		return &program, nil
	}
	scriptHost := &scripts.GameHost{Actors: scriptActors, Loops: &nativeLoops, Random: &nativeRandom, Env: scripts.GameHostEnv{
		CurrentSet: func() string { return activeSetName },
		Managed:    func(name string) bool { return scriptManaged[strings.ToLower(name)] },
		CastManaged: func(name string) bool {
			for _, cast := range []assets.Cast{gangCast, extraCast} {
				if strings.EqualFold(cast.ScriptName, name) {
					for _, actor := range cast.Actors {
						if scriptManaged[strings.ToLower(actor.Name)] {
							return true
						}
					}
				}
			}
			return false
		},
		CastScript: func(name string) (*scripts.Program, bool, error) {
			for _, cast := range []assets.Cast{gangCast, extraCast} {
				if strings.EqualFold(cast.ScriptName, name) {
					program, err := scriptProgram(cast.Name, cast.ScriptResource)
					return program, err == nil, err
				}
			}
			return nil, false, nil
		},
		ResolveStar: func(set, star string) ([3]int16, bool) {
			source := nightSet
			if strings.EqualFold(set, activeSetName) {
				source = activeSet
			} else if !strings.EqualFold(set, "town") {
				return [3]int16{}, false
			}
			position, found, err := source.ResolveLocation(star)
			return position, err == nil && found
		},
		Player: func() [3]int16 {
			return [3]int16{worldPoint[0]*256 + 128, worldPoint[1]*256 + 128, 0}
		},
		Camera: func() [3]int16 {
			camera := render.NativeActorCameraPosition(worldPoint)
			return [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		},
		ProjectedDepth: func(actor *scripts.ActorRecord) (int16, bool) {
			for _, projected := range projectedActors {
				if strings.EqualFold(projected.Name, actor.Name) {
					return int16(projected.Depth), true
				}
			}
			return 0, false
		},
		Heading:   render.NativeActorHeadingToPoint,
		Frame:     func() int32 { return int32(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())) },
		FrameRate: func() int32 { return 3 },
		OptionKey: func() bool { return ebiten.IsKeyPressed(ebiten.KeyAlt) },
		ShiftKey:  func() bool { return ebiten.IsKeyPressed(ebiten.KeyShift) },
		Chain: func(actor *scripts.ActorRecord) ([]scripts.ScriptFrame, error) {
			key := strings.ToLower(actor.Name)
			cast := scriptActorCasts[key]
			record, err := scriptResource(cast.Name, scriptActorRecords[key].Resource)
			if err != nil {
				return nil, err
			}
			if len(record) < 0x2a {
				return nil, fmt.Errorf("actor %s record is shorter than its script field", actor.Name)
			}
			own, err := scriptProgram(cast.Name, binary.LittleEndian.Uint32(record[0x26:0x2a]))
			if err != nil {
				return nil, err
			}
			shared, err := scriptProgram(cast.Name, cast.ScriptResource)
			if err != nil {
				return nil, err
			}
			return []scripts.ScriptFrame{
				{Program: own, Me: actor.Name, Target: actor.Name, Label: "Actor Script: "},
				{Program: shared, Me: actor.Name, Target: actor.Name, Label: "Cast Script: ", Last: true},
			}, nil
		},
	}}
	scriptInterpreter := scripts.NewInterpreter(scriptHost)
	// deliverScriptEvent raises an event the native way. A non-zero status is
	// what the native engine reports in its error dialog and then continues
	// past; an opcode the host lacks is an evidence gap. Both are logged and
	// play continues.
	// The engine owns these globals in Go state; scripts read them, and the
	// ones scripts write are read back after each run.
	sharedPhases := []struct {
		name string
		get  func() int32
		set  func(int32)
	}{
		{"bloodphase", func() int32 { return int32(bloodPhase) }, func(v int32) { bloodPhase = int16(v) }},
		{"docphase", func() int32 { return int32(docPhase) }, func(v int32) { docPhase = int16(v) }},
		{"trotterphase", func() int32 { return int32(trotterPhase) }, func(v int32) { trotterPhase = int16(v) }},
		{"mariephase", func() int32 { return int32(mariePhase) }, func(v int32) { mariePhase = int16(v) }},
		{"jonesphase", func() int32 { return int32(jonesPhase) }, func(v int32) { jonesPhase = int16(v) }},
		{"laurelphase", func() int32 { return int32(laurelPhase) }, func(v int32) { laurelPhase = int16(v) }},
		{"helpphase", func() int32 { return int32(helpPhase) }, func(v int32) { helpPhase = int16(v) }},
		{"isaophase", func() int32 { return int32(isaoPhase) }, func(v int32) { isaoPhase = int16(v) }},
	}
	publishScriptGlobals := func() error {
		for _, shared := range sharedPhases {
			if err := scriptInterpreter.SetGlobalNumber(shared.name, shared.get()); err != nil {
				return err
			}
		}
		for name, value := range map[string]int32{"day": int32(gameDay), "clock": int32(gameClock), "phase": int32(phase), "handflag": int32(handFlag), "playercash": playercash} {
			if err := scriptInterpreter.SetGlobalNumber(name, value); err != nil {
				return err
			}
		}
		if err := scriptInterpreter.SetGlobalString("handitem", handItem); err != nil {
			return err
		}
		if _, declared, err := scriptInterpreter.GlobalValue("playerdeath"); err != nil {
			return err
		} else if !declared {
			// advanceday (NEW.FLT) clears it on every day change.
			return scriptInterpreter.SetGlobalString("playerdeath", "")
		}
		return nil
	}
	collectScriptGlobals := func() error {
		for _, shared := range sharedPhases {
			if value, declared, err := scriptInterpreter.GlobalValue(shared.name); err != nil {
				return err
			} else if declared && value.Kind == 4 {
				shared.set(value.Int)
			}
		}
		if value, declared, err := scriptInterpreter.GlobalValue("handflag"); err != nil {
			return err
		} else if declared && value.Kind == 4 {
			handFlag = int16(value.Int)
		}
		if value, declared, err := scriptInterpreter.GlobalValue("playercash"); err != nil {
			return err
		} else if declared && value.Kind == 4 {
			playercash = value.Int
		}
		return nil
	}
	runScript := func(label, source string) error {
		if err := publishScriptGlobals(); err != nil {
			return err
		}
		status, err := scriptHost.Run(scriptInterpreter, source)
		if collectErr := collectScriptGlobals(); err == nil {
			err = collectErr
		}
		if errors.Is(err, scripts.ErrHostOpcodeUnimplemented) || errors.Is(err, scripts.ErrNotInTask) {
			log.Printf("script-gap %s: %v", label, err)
			return nil
		}
		if err != nil {
			return fmt.Errorf("script %s: %w", label, err)
		}
		if status != 0 {
			log.Printf("script-status %s status=%#x record=%d", label, status, scriptInterpreter.ProgramCounter)
		} else if *debug && *debugLoops {
			log.Printf("script-run %s", label)
		}
		return nil
	}
	deliverScriptEvent := func(event scripts.ActorEvent) error {
		return runScript(event.Actor+" "+event.Message, fmt.Sprintf("sendtoactor(%q,%s)", event.Actor, event.Message))
	}
	// initall (NEW.FLT) stops the world's loops and walks and then has the
	// cast initialize every actor for the day. Only script-managed actors are
	// reset here; the rest still run their hand-written setup.
	runInitActors := func() error {
		for key := range scriptManaged {
			nativeLoops.Stop(scripts.LoopKindActor, key)
			if record, status := scriptActors.Lookup(key); status == 0 {
				record.StopJob()
			}
		}
		return runScript("initactors", `sendtocast("gang",initactors())`)
	}
	// NEW.FLT initall runs the cast's initactors at the start of every day;
	// the first run happens here, before the opening scene.
	if err := runInitActors(); err != nil {
		stage.Close()
		return err
	}
	// # The standing pose, and why it is *not* baked into the base
	//
	// A conversation's base deliberately leaves the speaker's own world sprite out, because
	// the puppet stands in for it while a line plays. That left the actor present only while
	// a line was playing, so a choice list showed nobody.
	//
	// The obvious fix — composite the puppet's starting pose into the base — is **wrong**,
	// and it was tried first. The base is what `PuppetDialogue` restores *behind* the
	// puppet on every dirty region, so a base carrying a figure has that figure restored
	// underneath the animated one: a transparent region of the animated frame reveals the
	// pose, the actor appears twice, and the restore puts standing pixels where the scene
	// should be.
	//
	// So the base stays **scene only**, and the figure is always an explicit step on top of
	// it, drawn by whichever layer owns the moment: the dialogue while a line plays, the
	// choice renderer when nobody is speaking. One figure, never two.
	//
	// The pose is row 0 of a cue table — the pose a line begins from. It is a *per-line*
	// pose rather than one global neutral: on `LEROY.PUP`, 63 of 73 lines open with frame 0
	// in every slot and ten open in a variant, which is why the original symptom looked
	// intermittent. Where the caller knows which line the conversation will speak it passes
	// that name; where it does not, the first line in the table stands in.
	standingPoseOver := func(base render.IndexedFrame, puppet *render.Puppet, table assets.PuppetSpeechTable, speech []string, who string) (render.IndexedFrame, error) {
		if puppet == nil {
			return base, nil
		}
		wanted := ""
		if len(speech) > 0 {
			wanted = speech[0]
		}
		cue, ok := uint32(0), false
		for _, entry := range table.Entries {
			if wanted == "" || strings.EqualFold(entry.Name, wanted) {
				cue, ok = entry.CueResource, true
				break
			}
		}
		if wanted != "" && !ok {
			return render.IndexedFrame{}, fmt.Errorf("%s speech %q is missing from the line table", who, wanted)
		}
		if !ok {
			// No line to take a pose from, so there is no figure to stand in. That is the
			// one case with nobody on stage, and it is a conversation with nothing to say.
			return base, nil
		}
		pose, err := puppet.RestingFrame(base, cue)
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("compose %s standing pose: %w", who, err)
		}
		return pose, nil
	}
	// withPuppetPalette applies the puppet's palette to a base without drawing anything, so
	// a conversation's base carries the colours its figures will be drawn in and nothing
	// more. **It must not draw the figure** — see standingPoseOver.
	withPuppetPalette := func(base render.IndexedFrame, puppet *render.Puppet, who string) (render.IndexedFrame, error) {
		if puppet == nil {
			return base, nil
		}
		palette, err := puppet.Palette()
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("load %s PUP CLUT: %w", who, err)
		}
		base.Palette = palette
		return base, nil
	}
	composeMainPanel := func(worldBackground, panel render.IndexedFrame) (render.IndexedFrame, error) {
		frame, err := render.CompositeUnderlay(worldBackground, panel)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		portrait, anchor, err := avatarFrameForCurrent()
		if err != nil {
			return render.IndexedFrame{}, fmt.Errorf("load avatar frame %s/%d/%d: %w", avatarViewName, avatarFrameIndex, avatarAngle, err)
		}
		frame, err = render.CompositePuppetFrame(frame, portrait, anchor)
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
	startAvatarNoFace(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
	if *silent && *silentScript == "" && *loadPath == "" {
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
	movieAudio, movieAudioEvents, movieAudioLoop, err := startNativeMovieAudio(audioContext, movie)
	if err != nil {
		stage.Close()
		return err
	}
	movieAudioEmbedded := nativeMovieHasEmbeddedAudio(movie)
	var embeddedMovieAudioTails []*audio.Player
	releaseMovieAudio := func() error {
		if movieAudio == nil {
			return nil
		}
		if movieAudioEmbedded && movieAudio.IsPlaying() {
			embeddedMovieAudioTails = append(embeddedMovieAudioTails, movieAudio)
		} else if err := movieAudio.Close(); err != nil {
			return err
		}
		movieAudio, movieAudioEmbedded = nil, false
		return nil
	}
	drainMovieAudioTails := func() {
		active := embeddedMovieAudioTails[:0]
		for _, tail := range embeddedMovieAudioTails {
			if tail.IsPlaying() {
				active = append(active, tail)
			} else {
				_ = tail.Close()
			}
		}
		embeddedMovieAudioTails = active
	}
	defer func() {
		if movieAudio != nil {
			_ = movieAudio.Close()
		}
		for _, tail := range embeddedMovieAudioTails {
			_ = tail.Close()
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
	var renderMarieInventory func() error
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
		if *debug && activeSetName == "sallower" {
			projectedCount, trotterProjectedCount := 0, 0
			for _, actor := range projected {
				if strings.EqualFold(actor.Name, "Isao") {
					projectedCount++
					log.Printf("actor=isao projection depth=%d bounds=%d,%d,%d,%d", actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
				} else if strings.EqualFold(actor.Name, "Trotter") {
					trotterProjectedCount++
					log.Printf("actor=trotter projection depth=%d bounds=%d,%d,%d,%d", actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
				}
			}
			log.Printf("actor=isao scene-refresh set=%s view=%s point=%v visible=%t pose=%s heading=%d projected=%d", activeSetName, view.Name[1:], worldPoint, isaoVisible, actorPoses["isao"], actorHeadings["isao"], projectedCount)
			log.Printf("actor=trotter scene-refresh set=%s view=%s point=%v visible=%t pose=%s heading=%d projected=%d", activeSetName, view.Name[1:], worldPoint, trotterVisible, actorPoses["trotter"], actorHeadings["trotter"], trotterProjectedCount)
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
	startAvatarTiphat := func(after func() (bool, error)) error {
		if avatarTipActive {
			return nil
		}
		if gameClock == 3 {
			avatarViewName = "nitehattip"
		} else {
			avatarViewName = "dayhattip"
		}
		avatarFrameIndex, avatarAngle, avatarTipFrame = 0, 0, 0
		avatarTipActive, avatarIdleActive = true, false
		avatarTipAfter = after
		avatarGestureActive = false
		avatarTipNextFrame = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + nativeAvatarFrameInterval
		return refreshWorldScene()
	}
	advanceAvatarAnimation := func(now uint32) (bool, error) {
		if stage != mainStage {
			return false, nil
		}
		refresh := func() error {
			if currentScene == 2 {
				return renderMarieInventory()
			}
			return refreshWorldScene()
		}
		if avatarTipActive && now >= avatarTipNextFrame {
			avatarTipFrame++
			if avatarTipFrame == 26 {
				avatarTipActive = false
				startAvatarNoFace(now)
			} else {
				avatarFrameIndex, avatarTipNextFrame = avatarTipFrame, now+nativeAvatarFrameInterval
			}
			if err := refresh(); err != nil {
				return false, err
			}
			if !avatarTipActive && avatarTipAfter != nil {
				after := avatarTipAfter
				avatarTipAfter = nil
				if _, err := after(); err != nil {
					return false, err
				}
			}
			return true, nil
		}
		if !avatarIdleActive || currentScene == 0 && (helpInteractionStage != helpInteractionIdle || leroyInteractionStage != leroyInteractionIdle || jonesInteractionStage != jonesInteractionIdle || isaoInteractionStage != isaoInteractionIdle || marieInteractionStage != marieInteractionIdle || trotterInteractionStage != trotterInteractionIdle) {
			return false, nil
		}
		if now < avatarIdleNext {
			if avatarGestureActive && now >= avatarGestureNext {
				avatarFrameIndex, avatarGestureNext = (avatarFrameIndex+1)%26, now+nativeAvatarFrameInterval
				if err := refresh(); err != nil {
					return false, err
				}
				return true, nil
			}
			return false, nil
		}
		if !avatarMakefaceDue {
			startAvatarNoFace(now)
			if err := refresh(); err != nil {
				return false, err
			}
			return true, nil
		}
		face := nativeRandom.Inclusive(10)
		delay := 0
		switch face {
		case 1, 2, 3, 4:
			avatarViewName, avatarFrameIndex, avatarAngle = map[bool]string{true: "nitefaces", false: "dayfaces"}[gameClock == 3], 0, int16(face)
			delay = [...]int{0, 10, 10, 16, 12}[face]
		case 5:
			avatarViewName, avatarFrameIndex, avatarAngle = "dayrite", 0, 0
			if gameClock == 3 {
				avatarViewName = "niterite"
			}
			avatarGestureActive, avatarGestureNext = true, now+nativeAvatarFrameInterval
			delay = 24
		case 6:
			avatarViewName, avatarFrameIndex, avatarAngle = "dayleft", 0, 0
			if gameClock == 3 {
				avatarViewName = "niteleft"
			}
			avatarGestureActive, avatarGestureNext = true, now+nativeAvatarFrameInterval
			delay = 24
		case 10:
			avatarViewName, avatarFrameIndex, avatarAngle = map[bool]string{true: "nitefaces", false: "dayfaces"}[gameClock == 3], 0, 5
			delay = 3
		default:
			avatarIdleNext = now + nativeAvatarFrameInterval*uint32(nativeRandom.Inclusive(30)+30)
			return false, nil
		}
		avatarMakefaceDue, avatarIdleNext = false, now+nativeAvatarFrameInterval*uint32(delay)
		if err := refresh(); err != nil {
			return false, err
		}
		return true, nil
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
	setupTrotterActor := func(where string) error {
		setup, found := scripts.NativeTrotterActorSetup(where)
		if !found {
			return fmt.Errorf("unknown native Trotter setup %q", where)
		}
		position := setup.Position
		if !setup.HasPosition {
			set := activeSet
			if setup.Set == "town" {
				set = nightSet
			} else if setup.Set != activeSetName {
				var err error
				set, err = workspace.OpenSet("DATA/" + strings.ToUpper(setup.Set) + ".SET")
				if err != nil {
					return err
				}
				defer set.Close()
			}
			var err error
			position, found, err = set.ResolveLocation(setup.Star)
			if err != nil || !found {
				return fmt.Errorf("resolve Trotter star %q: found=%t err=%v", setup.Star, found, err)
			}
		}
		trotterActorSet, trotterStar, trotterPosition, trotterVisible, trotterFrameIndex = setup.Set, setup.Star, position, setup.Visible, 0
		trotterScale, trotterSpeed, trotterTurnSpeed, trotterZClip = setup.Scale, setup.Speed, setup.TurnSpeed, setup.ZClip
		actorPoses["trotter"] = setup.Pose
		if setup.SetHeading {
			actorHeadings["trotter"] = setup.Heading
		}
		trotterWalk = nil
		nativeLoops.Stop(scripts.LoopKindActor, "trotter")
		return nil
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
		hotelSceneAction, openHotelScene := scripts.HotelOpenSceneAction(semanticName, nextView.Resource, scripts.HotelStoryState{Day: gameDay, Clock: gameClock, Phase: gamePhase, FearPhase: fearPhase, LaurelPhase: laurelPhase, InventoryOwners: inventoryOwners, SavedScene: hotelSavedScene, SavedDirection: hotelSavedDirection})
		if openHotelScene {
			direction = hotelSceneAction.Direction
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
		trotterState := scripts.TrotterStoryState{Day: int16(gameDay), Clock: int16(gameClock), Phase: gamePhase, TrotterPhase: trotterPhase, TrotterActorSet: trotterActorSet, DocPhase: docPhase}
		closeTrotter := scripts.NativeTrotterSetTransition(trotterState, previousName, false)
		if closeTrotter.Hide {
			trotterVisible, trotterWalk = false, nil
			nativeLoops.Stop(scripts.LoopKindActor, "trotter")
		}
		if previousName == "town" && semanticName != "town" {
			townReturnScene = string(view.Name[1:])
			nativeLoops.Stop(scripts.LoopKindScene, "scene g14")
			for _, owner := range []string{"leroy", "dog", "help", "jones", "marie", "isao"} {
				nativeLoops.Stop(scripts.LoopKindActor, owner)
			}
			nativeLoops.Stop(scripts.LoopKindActor, "laurel")
			leroyWalk, helpWalk, jonesWalk = nil, nil, nil
			marieWalk = nil
			actorPoses["leroy"], actorPoses["help"], actorPoses["jones"] = "stand", "stand", "stand"
			actorTurnActive, helpTurnActive, jonesTurnActive = false, false, false
			marieTurnActive = false
		}
		activeSet, activeSetName, activeSetOwned = nextSet, semanticName, nextOwned
		view, worldPoint, backgroundFrame = nextView, nextPoint, background
		scriptHost.OpenSet()
		doorOwner = ""
		if previousName == "hotupper" && semanticName != "hotupper" {
			hotelJonesVisible, hotelJonesWalk = false, nil
			nativeLoops.Stop(scripts.LoopKindScene, "scene c4")
		}
		if semanticName == "hotupper" && strings.EqualFold(pendingHotelActorSetup, "jones") && strings.EqualFold(pendingHotelActorSelector, "hallway") {
			position, found, err := nextSet.ResolveLocation("hotupper.jones1")
			if err != nil || !found {
				return render.IndexedFrame{}, fmt.Errorf("resolve Jones hallway star hotupper.jones1: found=%t err=%v", found, err)
			}
			hotelJonesPosition, hotelJonesVisible, hotelJonesWalk = position, true, nil
			actorPoses["jones"], actorHeadings["jones"] = "stand", 64
			pendingHotelActorSetup, pendingHotelActorSelector = "", ""
			if *debug {
				log.Printf("actor=jones setup=hallway set=hotupper star=hotupper.jones1 heading=64 point=%v", hotelJonesPosition)
			}
		}
		if semanticName == "hotupper" && pendingHotelActorSetup != "" && scriptManaged[strings.ToLower(pendingHotelActorSetup)] {
			// HOTROOM's door sends the day's hallway actor its own setup.
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: pendingHotelActorSetup, Message: fmt.Sprintf("setupactor(%q)", pendingHotelActorSelector)}); err != nil {
				return render.IndexedFrame{}, err
			}
			pendingHotelActorSetup, pendingHotelActorSelector = "", ""
		}
		openTrotter := scripts.NativeTrotterSetTransition(trotterState, semanticName, true)
		if openTrotter.Setup != "" {
			if err := setupTrotterActor(openTrotter.Setup); err != nil {
				return render.IndexedFrame{}, err
			}
		}
		if openTrotter.Hide {
			trotterVisible, trotterWalk = false, nil
			nativeLoops.Stop(scripts.LoopKindActor, "trotter")
		}
		if openTrotter.SetGamePhaseValid {
			gamePhase = openTrotter.SetGamePhase
		}
		if semanticName == "sallower" {
			position, found, err := nextSet.ResolveLocation("sallower.isao")
			if err != nil || !found {
				return render.IndexedFrame{}, fmt.Errorf("resolve Sallowers Isao position: point=%v found=%t err=%v", position, found, err)
			}
			isaoPosition, isaoVisible, actorPoses["isao"], actorHeadings["isao"] = position, true, "stand", 64
			isaoBouncer, isaoDirGo, jonesTurnActive = false, false, false
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "isao", Callback: "isaoidle", Remaining: 2}); status != 0 {
				return render.IndexedFrame{}, fmt.Errorf("register Isao idle loop returned status %#x", status)
			}
		} else {
			isaoVisible = false
			nativeLoops.Stop(scripts.LoopKindActor, "isao")
		}
		if semanticName == "hotlower" && gameDay == 2 && gameClock == 1 && !laurelVisible && laurelPosition == ([3]int16{}) {
			if setup, found := scripts.NativeLaurelActorSetup("hotel"); found {
				position, found, err := nextSet.ResolveLocation(setup.Star)
				if err != nil || !found {
					return render.IndexedFrame{}, fmt.Errorf("resolve Laurel hotel star %q: found=%t err=%v", setup.Star, found, err)
				}
				laurelPosition, laurelVisible, actorPoses["laurel"], actorHeadings["laurel"] = position, setup.Visible, setup.Pose, setup.Heading
				laurelFrameIndex = 0
				if setup.Callback != "" {
					if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "laurel", Callback: setup.Callback, Remaining: setup.LoopTicks}); status != 0 {
						return render.IndexedFrame{}, fmt.Errorf("register Laurel idle callback: %#x", status)
					}
				}
			}
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
		if openHotelScene {
			hotelSceneOpenAction, hotelSceneOpenStep, hotelEventsLocked = &hotelSceneAction, 0, hotelSceneAction.LockEvents
			loopOwner := strings.ToLower(strings.TrimSpace(hotelSceneAction.LoopName + " " + strings.TrimPrefix(hotelSceneAction.LoopScene, "Scene ")))
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindScene, Owner: loopOwner, Callback: hotelSceneAction.LoopCode, Remaining: int32(hotelSceneAction.LoopTicks)}); status != 0 {
				return render.IndexedFrame{}, fmt.Errorf("register hotel scene-open trigger returned status %#x", status)
			}
			if *debug {
				log.Printf("hotel-open set=%s scene=%s resource=%d direction=%d lockevents=%t loop=%s/%s ticks=%d", semanticName, nextView.Name[1:], nextView.Resource, direction, hotelEventsLocked, hotelSceneAction.LoopName, hotelSceneAction.LoopCode, hotelSceneAction.LoopTicks)
			}
		} else if semanticName != "hotupper" {
			hotelSceneOpenAction, hotelEventsLocked = nil, false
		}
		if semanticName == "town" && previousName != "town" {
			if gameClock == 3 {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "leroy", Callback: "leroyidle", Remaining: 20}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Leroy idle loop after town return returned status %#x", status)
				}
			}
			if marieVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Marie idle loop after town return returned status %#x", status)
				}
			}
			if gameDay == 1 && dogVisibleState {
				step, found := scripts.DogIdleStep("doright", &nativeRandom)
				if found {
					actorPoses["dog"] = step.Pose
					if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "dog", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
						return render.IndexedFrame{}, fmt.Errorf("register Dog idle loop after town return returned status %#x", status)
					}
				}
			}
			if helpVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "help", Callback: "helpidle", Remaining: 19}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Help idle loop after town return returned status %#x", status)
				}
			}
			if jonesVisible && jonesWalk == nil {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Jones idle loop after town return returned status %#x", status)
				}
			}
			if currentThemeName == "nightwind3" {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindScene, Owner: "scene g14", Callback: "nightfxs", Remaining: 2}); status != 0 {
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
			viewIndex := -1
			for index, candidate := range activeSet.Views() {
				if candidate.SceneID == view.SceneID && candidate.DirectionID == view.DirectionID {
					viewIndex = index
					break
				}
			}
			log.Printf("set=%s view=%s direction=%d view-index=%d resource=%d size=%dx%d", setName, view.Name[1:], direction, viewIndex, resource, background.Width, background.Height)
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
		nativeLoops.Stop(scripts.LoopKindActor, "help")
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
		if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "help", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
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
		nativeLoops.Stop(scripts.LoopKindActor, "help")
		if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "help", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
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
		nativeLoops.Stop(scripts.LoopKindActor, "help")
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
		leroyPuppet, leroyPuppetTable, leroyChoices = puppet, table, choices
		return nil
	}
	// leroyCueResourceFor is no longer needed: `withRestingPuppet` resolves the cue.
	var showLeroyChoices func() error
	startLeroyBySign := func() error {
		if err := openLeroyPuppet(); err != nil {
			return fmt.Errorf("open Leroy dialogue: %w", err)
		}
		// **No world actor survives into a conversation.** The puppet fills the 512x264
		// viewport as a foreground close-up, so a bystander left standing behind it appears
		// inside the portrait -- the speaker's giant face with a smaller figure showing
		// through it. The room and the panel stay; the cast does not.
		dialogueActors := make([]render.WorldActorSprite, 0)
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
		bySign := scripts.LeroyBySignEntryForPhase(leroyPhase)
		leroyStandingSpeech = bySign.Speech
		// **The base stays scene only.** The figure is added on top, by the dialogue while
		// a line plays and by the choice renderer when nobody is speaking.
		leroyConversationBase, err = withPuppetPalette(leroyConversationBase, leroyPuppet, "Leroy")
		if err != nil {
			return err
		}
		if leroyDialogue != nil {
			if err := leroyDialogue.Close(); err != nil {
				return err
			}
			leroyDialogue = nil
		}
		entry := bySign
		leroyDialogueRepeats, leroyDialogueReturns, leroyDialogueSetsPhase = entry.Repeats, entry.Returns, entry.SetPhase
		if len(entry.Speech) == 0 {
			currentFrame, stageFrame = leroyConversationBase, leroyConversationBase
			return showLeroyChoices()
		}
		dialogue, err := engine.NewPuppetDialogue(leroyPuppet, leroyPuppetTable.Entries, entry.Speech, audioContext)
		if err != nil {
			return err
		}
		leroyDialogue = dialogue
		frame, err := leroyDialogue.Start(leroyConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			return fmt.Errorf("start Leroy bysign dialogue: %w", err)
		}
		currentFrame, stageFrame = frame, frame
		leroyInteractionStage = leroyInteractionPuppetSpeaking
		if *debug {
			log.Printf("puppet=leroy script=bysign phase=%d lines=%d choices=%d", leroyPhase, len(entry.Speech), len(leroyChoices))
		}
		return nil
	}
	drawLeroyChoices := func(outline int) error {
		labels := make([]string, len(leroyActiveChoices))
		for index, choice := range leroyActiveChoices {
			labels[index] = choice.Text
		}
		// **The choice screen draws the standing pose over the scene, not the last animated
		// frame.** Reusing `currentFrame` would put the animated puppet's own pixels in the
		// base and then draw the pose on top of them, so the figure would appear twice. The
		// conversation base is scene only, and the pose is this layer's contribution.
		choiceBackground, err := standingPoseOver(leroyConversationBase,
			leroyPuppet, leroyPuppetTable, leroyStandingSpeech, "Leroy")
		if err != nil {
			return err
		}
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
	showLeroyChoices = func() error {
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
			leroyChoicePressActive = false
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
		if leroyDialogue != nil {
			if err := leroyDialogue.Close(); err != nil {
				return err
			}
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
		choiceBackground, err := standingPoseOver(helpConversationBase,
			helpPuppet, helpPuppetTable, nil, "Help")
		if err != nil {
			return err
		}
		frame, err := helpPuppet.ChoiceFrame(choiceBackground, helpPuppetTable.PanelResource, labels)
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
		helpChoicePressActive = false
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
		// **No world actor survives into a conversation.** The puppet fills the 512x264
		// viewport as a foreground close-up, so a bystander left standing behind it appears
		// inside the portrait -- the speaker's giant face with a smaller figure showing
		// through it. The room and the panel stay; the cast does not.
		dialogueActors := make([]render.WorldActorSprite, 0)
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
		helpConversationBase, err = withPuppetPalette(helpConversationBase, helpPuppet, "Help")
		if err != nil {
			return err
		}
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
		jonesPuppet, jonesPuppetTable, jonesPuppetProgram, jonesChoiceGroups, jonesPuppetCode = puppet, table, program, groups, "threenite"
		return nil
	}
	openTrotterPuppet := func() error {
		if trotterPuppet != nil {
			return nil
		}
		puppet, err := render.OpenPuppet(workspace, "PUPPETS/TROTTER.PUP")
		if err != nil {
			return err
		}
		table, err := workspace.OpenPuppetSpeechTable("PUPPETS/TROTTER.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		trotterPuppet, trotterPuppetTable = puppet, table
		return nil
	}
	buildTrotterConversationBase := func() error {
		if trotterPuppet == nil {
			return fmt.Errorf("Trotter PUP is unavailable")
		}
		// **No world actor survives into a conversation.** The puppet fills the 512x264
		// viewport as a foreground close-up, so a bystander left standing behind it appears
		// inside the portrait -- the speaker's giant face with a smaller figure showing
		// through it. The room and the panel stay; the cast does not.
		dialogueActors := make([]render.WorldActorSprite, 0)
		dialogueBackground, _, err := compositeWorld(backgroundFrame, worldPoint, dialogueActors)
		if err != nil {
			return fmt.Errorf("render Trotter dialogue background: %w", err)
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return fmt.Errorf("render Trotter dialogue panel: %w", err)
		}
		base, err := composeMainPanel(dialogueBackground, panel)
		if err != nil {
			return fmt.Errorf("compose Trotter dialogue scene: %w", err)
		}
		palette, err := trotterPuppet.Palette()
		if err != nil {
			return fmt.Errorf("load Trotter PUP palette: %w", err)
		}
		base.Palette = palette
		base, err = withPuppetPalette(base, trotterPuppet, "Trotter")
		if err != nil {
			return err
		}
		trotterConversationBase = base
		return nil
	}
	trotterStoryState := func() (scripts.TrotterStoryState, error) {
		state := scripts.TrotterStoryState{Day: int16(gameDay), Clock: int16(gameClock), TrotterPhase: trotterPhase, Phase: gamePhase, PlayerCash: playercash, Counter: trotterGiftCounter, TrotterActorValue: trotterActorValue, BloodPhase: bloodPhase, GunOwner: inventoryOwners["gun"], SugarcubesOwner: inventoryOwners["sugarcubes"], ThunderbirdOwner: inventoryOwners["tbird"], TrotterActorSet: trotterActorSet, DocPhase: docPhase}
		if len(marieInventoryProgram.Records) == 0 {
			data, err := inventoryArchive.Resource(1)
			if err != nil {
				return state, err
			}
			marieInventoryProgram, err = scripts.ParseProgram(data)
			if err != nil {
				return state, err
			}
			marieHandBevelChoices, err = scripts.PuppetBevelChoices(marieInventoryProgram, "addhandbevel")
			if err != nil {
				return state, err
			}
		}
		if handFlag == 1 && len(marieHandBevelChoices) > 0 {
			state.HandChoice = &marieHandBevelChoices[0]
		} else if !inventoryHidden[strings.ToLower(handItem)] {
			choice, found, err := scripts.PuppetBevelChoiceInCase(marieInventoryProgram, "addhandbevel", "handitem", handItem)
			if err != nil {
				return state, err
			}
			if found {
				state.HandChoice = &choice
			}
		}
		return state, nil
	}
	drawTrotterChoices := func(outline int) error {
		labels := make([]string, len(trotterActiveChoices))
		for index, choice := range trotterActiveChoices {
			labels[index] = choice.Text
		}
		background, err := standingPoseOver(trotterConversationBase, trotterPuppet, trotterPuppetTable, nil, "Trotter")
		if err != nil {
			return err
		}
		frame, err := trotterPuppet.ChoiceFrame(background, trotterPuppetTable.PanelResource, labels)
		if err != nil {
			return fmt.Errorf("render Trotter choice panel: %w", err)
		}
		if outline >= 0 {
			frame, err = render.DrawNativePuppetChoiceBevel(frame, outline)
			if err != nil {
				return err
			}
		}
		currentFrame, stageFrame, trotterChoiceOutline = frame, frame, outline
		if *debug && outline < 0 {
			for index, choice := range trotterActiveChoices {
				log.Printf("puppet=trotter choice-row=%d event=%d text=%q", index, choice.EventID, choice.Text)
			}
		}
		return nil
	}
	openLaurelPuppet := func() error {
		if laurelPuppet != nil {
			return nil
		}
		puppet, err := render.OpenPuppet(workspace, "PUPPETS/LAUREL.PUP")
		if err != nil {
			return err
		}
		table, err := workspace.OpenPuppetSpeechTable("PUPPETS/LAUREL.PUP")
		if err != nil {
			_ = puppet.Close()
			return err
		}
		controller, err := scripts.NewLaurelScriptController(workspace)
		if err != nil {
			_ = puppet.Close()
			return err
		}
		laurelPuppet, laurelSpeechTable, laurelController = puppet, table, controller
		return nil
	}
	buildLaurelConversationBase := func() error {
		background, _, err := compositeWorld(backgroundFrame, worldPoint, nil)
		if err != nil {
			return err
		}
		panel, err := render.StageFrame(stage, currentPixels.Pixels)
		if err != nil {
			return err
		}
		base, err := composeMainPanel(background, panel)
		if err != nil {
			return err
		}
		base.Palette, err = laurelPuppet.Palette()
		if err != nil {
			return err
		}
		laurelConversationBase, err = withPuppetPalette(base, laurelPuppet, "Laurel")
		return err
	}
	drawLaurelChoices := func(outline int) error {
		labels := make([]string, len(laurelChoices))
		for index, choice := range laurelChoices {
			labels[index] = choice.Text
			if *debug && outline < 0 {
				log.Printf("puppet=laurel choice-row=%d event=%d text=%q", index, choice.EventID, choice.Text)
			}
		}
		background, err := standingPoseOver(laurelConversationBase, laurelPuppet, laurelSpeechTable, nil, "Laurel")
		if err != nil {
			return err
		}
		frame, err := laurelPuppet.ChoiceFrame(background, laurelSpeechTable.PanelResource, labels)
		if err != nil {
			return err
		}
		if outline >= 0 {
			frame, err = render.DrawNativePuppetChoiceBevel(frame, outline)
			if err != nil {
				return err
			}
		}
		currentFrame, stageFrame, laurelChoiceOutline = frame, frame, outline
		return nil
	}
	laurelStoryState := func() (scripts.LaurelStoryState, error) {
		state := scripts.LaurelStoryState{Day: int16(gameDay), Clock: int16(gameClock), LaurelPhase: laurelPhase, LaurelGood: laurelGood, LaurelActorValue: laurelActorValue, Counter: laurelCounter, JonesOwner: inventoryOwners["message"], HankerchiefOwner: inventoryOwners["hankerchief"], HankerchiefDegree: hankerchiefDegree}
		if len(marieInventoryProgram.Records) == 0 {
			data, err := inventoryArchive.Resource(1)
			if err != nil {
				return state, err
			}
			marieInventoryProgram, err = scripts.ParseProgram(data)
			if err != nil {
				return state, err
			}
			marieHandBevelChoices, err = scripts.PuppetBevelChoices(marieInventoryProgram, "addhandbevel")
			if err != nil {
				return state, err
			}
		}
		if handFlag == 1 && len(marieHandBevelChoices) > 0 {
			state.HandChoice = &marieHandBevelChoices[0]
		} else {
			choice, found, err := scripts.PuppetBevelChoiceInCase(marieInventoryProgram, "addhandbevel", "handitem", handItem)
			if err != nil {
				return state, err
			}
			if found && !inventoryHidden[strings.ToLower(handItem)] {
				state.HandChoice = &choice
			}
		}
		return state, nil
	}
	var startLaurelStep func(scripts.LaurelStep) error
	applyLaurelEffects := func(step scripts.LaurelStep) error {
		for _, effect := range step.Effects {
			switch effect.Kind {
			case scripts.LaurelEffectSetStoryValue:
				switch strings.ToLower(effect.Target) {
				case "laurelphase":
					laurelPhase = int16(effect.Amount)
				case "laurelgood":
					laurelGood = int16(effect.Amount)
				case "oonakidstory":
					oonakidstory = effect.Amount
				case "counter":
					laurelCounter = effect.Amount
				default:
					return fmt.Errorf("unknown Laurel story value %q", effect.Target)
				}
			case scripts.LaurelEffectAddInventory, scripts.LaurelEffectGiveInventory:
				item := strings.ToLower(effect.Value)
				owner := "stranger"
				if effect.Kind == scripts.LaurelEffectGiveInventory {
					owner = effect.Target
					inventoryHidden[item] = true
					if strings.EqualFold(item, handItem) {
						handItem = ""
					}
				}
				inventoryOwners[item] = owner
				if _, found := inventoryLargeFrames[item]; !found {
					frame, err := loadInventoryFrame(inventoryArchive, effect.Value, "large", 0)
					if err != nil {
						return err
					}
					inventoryLargeFrames[item] = frame
				}
				if owner == "stranger" {
					handItem = effect.Value
					if err := soundBank.Play(audioContext, "inven", 1); err != nil {
						return err
					}
				}
			case scripts.LaurelEffectMoveActor:
				position, found, err := activeSet.ResolveLocation(effect.Value)
				if err != nil || !found {
					return fmt.Errorf("resolve Laurel destination %q: found=%t err=%v", effect.Value, found, err)
				}
				start := laurelPosition
				heading := render.NativeActorHeadingToPoint(start, position)
				walk := scripts.NewNativeActorWalkJob(start, position, heading, 4)
				laurelWalk, actorPoses["laurel"], laurelWalkNext = &walk, "walk", scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
				laurelVisible, laurelFrameIndex = true, 0
			case scripts.LaurelEffectHideActor:
				laurelVisible, laurelWalk = false, nil
				nativeLoops.Stop(scripts.LoopKindActor, "laurel")
			case scripts.LaurelEffectPlayerDeath:
				return finishPlayerDeath(effect.Value)
			case scripts.LaurelEffectSelectHand:
				if handItem == "" || handFlag == 1 {
					return openLaurelInventory()
				}
				state, err := laurelStoryState()
				if err != nil {
					return err
				}
				step, err := laurelController.BeginGift(state, handItem, laurelContinuation, &nativeRandom)
				if err != nil {
					return err
				}
				inventoryHidden[strings.ToLower(handItem)], handItem = true, ""
				return startLaurelStep(step)
			}
		}
		return nil
	}
	var advanceLaurelSpeech func() error
	advanceLaurelSpeech = func() error {
		if laurelDialogue != nil {
			if err := laurelDialogue.Close(); err != nil {
				return err
			}
			laurelDialogue = nil
		}
		if len(laurelPendingStep.Choices) > 0 {
			if err := applyLaurelEffects(laurelPendingStep); err != nil {
				return err
			}
			laurelChoices, laurelContinuation = append([]scripts.PuppetChoice(nil), laurelPendingStep.Choices...), laurelPendingStep.Continuation
			laurelChoicePressActive, laurelChoicePressIndex, laurelChoiceUntil = false, -1, 0
			if laurelPendingStep.ChoiceTimeoutFrames > 0 {
				laurelChoiceUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + laurelPendingStep.ChoiceTimeoutFrames
			}
			laurelInteractionStage = laurelInteractionChoices
			return drawLaurelChoices(-1)
		}
		if err := applyLaurelEffects(laurelPendingStep); err != nil {
			return err
		}
		if laurelPendingStep.Finish {
			laurelActorValue++
			if laurelWalk == nil {
				actorPoses["laurel"] = "stand"
			}
			laurelInteractionStage = laurelInteractionIdle
			if *debug {
				log.Printf("puppet=laurel complete phase=%d good=%d actorvalue=%d", laurelPhase, laurelGood, laurelActorValue)
			}
			return refreshWorldScene()
		}
		return nil
	}
	startLaurelStep = func(step scripts.LaurelStep) error {
		laurelPendingStep = step
		if err := openLaurelPuppet(); err != nil {
			return err
		}
		if err := buildLaurelConversationBase(); err != nil {
			return err
		}
		if len(step.Speech) == 0 {
			return advanceLaurelSpeech()
		}
		dialogue, err := engine.NewPuppetDialogue(laurelPuppet, laurelSpeechTable.Entries, step.Speech, audioContext)
		if err != nil {
			return err
		}
		frame, err := dialogue.Start(laurelConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			_ = dialogue.Close()
			return err
		}
		laurelDialogue, laurelInteractionStage = dialogue, laurelInteractionSpeaking
		currentFrame, stageFrame = frame, frame
		return nil
	}
	startLaurelChoice := func(event int32) error {
		state, err := laurelStoryState()
		if err != nil {
			return err
		}
		step, err := laurelController.Continue(state, laurelContinuation, event, &nativeRandom)
		if err != nil {
			return err
		}
		return startLaurelStep(step)
	}
	beginLaurelPuppetTalk := func() (bool, error) {
		if activeSetName != "hotlower" || !laurelVisible || laurelWalk != nil || laurelInteractionStage != laurelInteractionIdle {
			return false, nil
		}
		camera := render.NativeActorCameraPosition(worldPoint)
		player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		if scripts.NativeActorDistance2D(laurelPosition, player) >= townActorHotDistance {
			return false, nil
		}
		if err := openLaurelPuppet(); err != nil {
			return false, err
		}
		handFlag = 1
		state, err := laurelStoryState()
		if err != nil {
			return false, err
		}
		step, err := laurelController.BeginBreakfast(state, &nativeRandom)
		if err != nil {
			return false, err
		}
		if err := startLaurelStep(step); err != nil {
			return false, err
		}
		return true, nil
	}
	var finishTrotterStep func() error
	var startTrotterStep func(scripts.TrotterStep) error
	var startTrotterGift func() error
	var startTrotterSpeechStage func(bool) error
	startTrotterDialogue := func(step scripts.TrotterStep) error {
		if len(step.Speech) == 0 {
			return fmt.Errorf("Trotter phase %d has no speech", trotterPhase)
		}
		dialogue, err := engine.NewPuppetDialogue(trotterPuppet, trotterPuppetTable.Entries, step.Speech, audioContext)
		if err != nil {
			return err
		}
		frame, err := dialogue.Start(trotterConversationBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err != nil {
			_ = dialogue.Close()
			return err
		}
		trotterPendingStep, trotterDialogue, trotterInteractionStage = step, dialogue, trotterInteractionPuppetSpeaking
		currentFrame, stageFrame = frame, frame
		if *debug {
			log.Printf("puppet=trotter phase=%d speech-lines=%d", trotterPhase, len(step.Speech))
		}
		return nil
	}
	startTrotterSpeechStage = func(delayed bool) error {
		stage := trotterSpeechStages[trotterSpeechStageIndex]
		if stage.DelayBefore > 0 && !delayed {
			trotterDelayUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + stage.DelayBefore
			trotterInteractionStage = trotterInteractionPuppetDelay
			return nil
		}
		step := trotterPendingStep
		step.Speech = stage.Speech
		return startTrotterDialogue(step)
	}
	finishTrotterStep = func() error {
		step := trotterPendingStep
		sceneName, direction := string(view.Name[1:]), worldPoint[2]
		viewChanged := false
		for _, effect := range step.Effects {
			switch effect.Kind {
			case scripts.TrotterEffectAdjustCash:
				playercash += effect.Amount
				if *debug {
					log.Printf("puppet=trotter cash=%d delta=%d", playercash, effect.Amount)
				}
			case scripts.TrotterEffectGiveInventory:
				inventoryOwners[strings.ToLower(effect.Value)] = effect.Target
			case scripts.TrotterEffectSetCounter:
				trotterGiftCounter = effect.Amount
			case scripts.TrotterEffectSetPhase:
				gamePhase = effect.Phase
			case scripts.TrotterEffectAddInventory:
				item := strings.ToLower(effect.Value)
				inventoryOwners[item], inventoryHidden[item], handItem = "stranger", false, effect.Value
				if _, found := inventoryLargeFrames[item]; !found {
					frame, err := loadInventoryFrame(inventoryArchive, effect.Value, "large", 0)
					if err != nil {
						return err
					}
					inventoryLargeFrames[item] = frame
				}
				if err := soundBank.Play(audioContext, "inven", 1); err != nil {
					return err
				}
			case scripts.TrotterEffectSetStoryValue:
				switch effect.Target {
				case "docphase":
					docPhase = int16(effect.Amount)
				case "fighton":
					fightOn = int16(effect.Amount)
				case "bloodphase":
					bloodPhase = int16(effect.Amount)
				case "laurelphase":
					laurelPhase = int16(effect.Amount)
				case "laurelgood":
					laurelGood = int16(effect.Amount)
				case "oonakidstory":
					oonakidstory = effect.Amount
				case "jonesphase":
					jonesPhase = int16(effect.Amount)
				default:
					return fmt.Errorf("unknown Trotter story variable %q", effect.Target)
				}
			case scripts.TrotterEffectHideActor:
				if strings.EqualFold(effect.Target, "trotter") {
					trotterVisible, trotterWalk = false, nil
					nativeLoops.Stop(scripts.LoopKindActor, "trotter")
				} else if strings.EqualFold(effect.Target, "laurel") {
					laurelVisible, laurelWalk = false, nil
				} else {
					return fmt.Errorf("unknown Trotter hide target %q", effect.Target)
				}
			case scripts.TrotterEffectSetActorHeading:
				actorHeadings[strings.ToLower(effect.Target)] = effect.Heading
			case scripts.TrotterEffectPlayerDeath:
				if step.SetTrotterPhaseValid {
					trotterPhase = step.SetTrotterPhase
				}
				trotterPendingStep, trotterInteractionStage = scripts.TrotterStep{}, trotterInteractionIdle
				return finishPlayerDeath(effect.Value)
			case scripts.TrotterEffectMoveActor:
				if strings.EqualFold(effect.Target, "laurel") {
					position, found, err := activeSet.ResolveLocation(effect.Value)
					if err != nil || !found {
						return fmt.Errorf("resolve Laurel destination %q: found=%t err=%v", effect.Value, found, err)
					}
					laurelPosition, laurelVisible, actorPoses["laurel"] = position, true, "stand"
				} else if strings.EqualFold(effect.Target, "trotter") {
					position, found, err := activeSet.ResolveLocation(effect.Value)
					if err != nil || !found {
						return fmt.Errorf("resolve Trotter destination %q: found=%t err=%v", effect.Value, found, err)
					}
					heading := render.NativeActorHeadingToPoint(trotterPosition, position)
					walk := scripts.NewNativeActorWalkJob(trotterPosition, position, heading, trotterSpeed)
					trotterWalk, actorPoses["trotter"], trotterWalkNext = &walk, "stand", scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
					trotterFrameIndex = 0
				} else {
					return fmt.Errorf("unsupported Trotter actor target %q", effect.Target)
				}
			case scripts.TrotterEffectSetScene:
				sceneName, viewChanged = "scene "+strings.TrimPrefix(effect.Value, "scene "), true
			case scripts.TrotterEffectSetDirection:
				switch effect.Value {
				case "north":
					direction = assets.SetDirectionNorth
				case "east":
					direction = assets.SetDirectionEast
				case "south":
					direction = assets.SetDirectionSouth
				case "west":
					direction = assets.SetDirectionWest
				default:
					return fmt.Errorf("unknown Trotter direction %q", effect.Value)
				}
				viewChanged = true
			case scripts.TrotterEffectSelectHand:
				if handItem == "" || handFlag == 1 {
					return openTrotterInventory()
				}
				return startTrotterGift()
			default:
				return fmt.Errorf("unsupported Trotter effect %d", effect.Kind)
			}
		}
		step.Effects = nil
		trotterPendingStep = step
		if step.SetTrotterPhaseValid {
			trotterPhase = step.SetTrotterPhase
		}
		if len(step.Choices) > 0 {
			if step.Continuation != nil && step.Continuation.Kind == scripts.TrotterContinuationNativeEvent {
				state, err := trotterStoryState()
				if err != nil {
					return err
				}
				resumed, err := trotterScriptController.Resume(state, step.Continuation)
				if err != nil {
					return err
				}
				step.Choices, trotterPendingStep.Choices = resumed.Choices, resumed.Choices
				if resumed.Finish && len(resumed.Choices) == 0 {
					trotterPendingStep.Continuation = nil
					return finishTrotterStep()
				}
			}
			trotterActiveChoices = append([]scripts.PuppetChoice(nil), step.Choices[0]...)
			if step.ScrambleChoices {
				scripts.PuppetScrambleChoices(trotterActiveChoices, &nativeRandom)
			}
			trotterChoicePressActive, trotterChoicePressIndex = false, -1
			trotterChoiceUntil = 0
			if step.ChoiceTimeoutFrames > 0 {
				trotterChoiceUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + step.ChoiceTimeoutFrames
			}
			trotterInteractionStage = trotterInteractionPuppetChoices
			return drawTrotterChoices(-1)
		}
		if step.Continuation != nil {
			return fmt.Errorf("Trotter continuation %q has no choices", step.Continuation.Code)
		}
		trotterPendingStep, trotterActiveChoices, trotterInteractionStage = scripts.TrotterStep{}, nil, trotterInteractionIdle
		trotterActorValue++
		if trotterWalk == nil {
			actorPoses["trotter"] = "stand"
		}
		if viewChanged {
			_, err := setWorldView(sceneName, direction)
			return err
		}
		return refreshWorldScene()
	}
	startTrotterStep = func(step scripts.TrotterStep) error {
		trotterPendingStep = step
		trotterChoiceUntil, trotterSpeechStageIndex = 0, 0
		trotterSpeechStages = step.SpeechStages
		if len(trotterSpeechStages) == 0 && len(step.Speech) > 0 {
			trotterSpeechStages = []scripts.TrotterSpeechStage{{Speech: step.Speech}}
		}
		if len(trotterSpeechStages) > 0 {
			return startTrotterSpeechStage(false)
		}
		return finishTrotterStep()
	}
	startTrotterGift = func() error {
		state, err := trotterStoryState()
		if err != nil {
			return err
		}
		item := handItem
		step, err := trotterScriptController.BeginGift(state, item, trotterPendingStep.Continuation)
		if err != nil {
			return err
		}
		inventoryHidden[strings.ToLower(item)], handItem = true, ""
		if err := buildTrotterConversationBase(); err != nil {
			return err
		}
		return startTrotterStep(step)
	}
	startTrotterEventResponse := func(event int32) error {
		state, err := trotterStoryState()
		if err != nil {
			return err
		}
		step, err := trotterScriptController.Continue(state, trotterPendingStep.Continuation, event)
		if err != nil {
			return err
		}
		return startTrotterStep(step)
	}
	finishTrotterDialogue := func() error {
		if trotterDialogue != nil {
			if err := trotterDialogue.Close(); err != nil {
				return err
			}
			trotterDialogue = nil
		}
		trotterSpeechStageIndex++
		if trotterSpeechStageIndex < len(trotterSpeechStages) {
			return startTrotterSpeechStage(false)
		}
		return finishTrotterStep()
	}
	beginTrotterPuppetTalk := func() (bool, error) {
		if activeSetName != trotterActorSet || !trotterVisible || trotterWalk != nil || trotterInteractionStage != trotterInteractionIdle {
			return false, nil
		}
		camera := render.NativeActorCameraPosition(worldPoint)
		player := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
		distance := scripts.NativeActorDistance2D(trotterPosition, player)
		if distance >= townActorHotDistance {
			if *debug {
				log.Printf("actor=trotter mousedown=ignored distance=%d hotdist=%d", distance, townActorHotDistance)
			}
			return false, nil
		}
		if err := openTrotterPuppet(); err != nil {
			return false, fmt.Errorf("open Trotter dialogue: %w", err)
		}
		handFlag = 1
		if activeSetName == "sallower" {
			if _, err := setWorldView("scene c3", assets.SetDirectionWest); err != nil {
				return false, err
			}
		}
		if err := buildTrotterConversationBase(); err != nil {
			return false, err
		}
		if trotterScriptController == nil {
			trotterScriptController, err = scripts.NewTrotterScriptController(workspace)
			if err != nil {
				return false, fmt.Errorf("initialize Trotter script controller: %w", err)
			}
		}
		state, err := trotterStoryState()
		if err != nil {
			return false, err
		}
		step, err := trotterScriptController.Dispatch(state)
		if err != nil {
			return false, err
		}
		if step.Kind != scripts.TrotterActionDialogue {
			if *debug {
				log.Printf("actor=trotter unsupported-step kind=%d day=%d clock=%d phase=%d route=%s/%s", step.Kind, gameDay, gameClock, trotterPhase, step.Route.Page, step.Route.Code)
			}
			return false, nil
		}
		if err := startTrotterStep(step); err != nil {
			return false, fmt.Errorf("start Trotter phase %d: %w", trotterPhase, err)
		}
		if *debug {
			log.Printf("actor=trotter mousedown=accepted phase=%d distance=%d", trotterPhase, distance)
		}
		return true, nil
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
		choiceBackground, err := standingPoseOver(isaoConversationBase,
			isaoPuppet, isaoPuppetTable, nil, "Isao")
		if err != nil {
			return err
		}
		frame, err := isaoPuppet.ChoiceFrame(choiceBackground, isaoPuppetTable.PanelResource, labels)
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
		isaoChoicePressActive = false
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
		base, err = withPuppetPalette(base, isaoPuppet, "Isao")
		if err != nil {
			return err
		}
		isaoConversationBase = base
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
	resumeIsaoTheme := func() error {
		if !isaoThemeResume {
			return nil
		}
		if themePlayer != nil {
			if err := themePlayer.Close(); err != nil {
				return err
			}
			themePlayer = nil
		}
		if currentTheme.Name == "" || audioContext == nil {
			return fmt.Errorf("Isao saved theme state is unavailable")
		}
		player, err := currentTheme.Play(audioContext)
		if err != nil {
			return err
		}
		themePlayer, isaoThemeResume = player, false
		return nil
	}
	returnIsaoToIdle := func() error {
		isaoSecondRun, isaoInteractionStage = false, isaoInteractionIdle
		actorPoses["isao"] = "stand"
		if isaoVisible && activeSetName == "sallower" {
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "isao", Callback: "isaoidle", Remaining: 2}); status != 0 {
				return fmt.Errorf("register Isao idle loop after dialogue returned status %#x", status)
			}
		}
		if err := resumeIsaoTheme(); err != nil {
			return fmt.Errorf("resume Isao theme: %w", err)
		}
		if *debug {
			log.Printf("actor=isao mousedown-complete actorvalue=%d isaophase=%d visible=%t", isaoActorValue, isaoPhase, isaoVisible)
		}
		return refreshWorldScene()
	}
	finishIsaoPuppetRun = func() error {
		if isaoDialogue != nil {
			if err := isaoDialogue.Close(); err != nil {
				return err
			}
			isaoDialogue = nil
		}
		if err := refreshWorldScene(); err != nil {
			return fmt.Errorf("restore Sallower after Isao PUP run: %w", err)
		}
		if !isaoSecondRun {
			isaoActorValue++
			if gameDay == 3 && gameClock == 1 {
				isaoKeyMoviePending = true
				isaoDelayUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + 60
				isaoInteractionStage = isaoInteractionPuppetDelay
				return nil
			}
			return returnIsaoToIdle()
		}
		isaoActorValue++
		return returnIsaoToIdle()
	}
	startIsaoEventResponse = func(event int32) error {
		state := scripts.IsaoState{Day: gameDay, Clock: gameClock, Phase: gamePhase, IsaoPhase: isaoPhase, TrotterPhase: trotterPhase, OonaActorValue: oonaActorValue, RingOwner: inventoryOwners["ring"]}
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
		state := scripts.IsaoState{Day: gameDay, Clock: gameClock, Phase: gamePhase, IsaoPhase: isaoPhase, TrotterPhase: trotterPhase, OonaActorValue: oonaActorValue, RingOwner: inventoryOwners["ring"]}
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
		currentFrame, stageFrame = isaoConversationBase, isaoConversationBase
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
		isaoThemeResume = themePlayer != nil && currentTheme.Name != ""
		if themePlayer != nil {
			if err := themePlayer.Close(); err != nil {
				return false, fmt.Errorf("halt Isao background theme: %w", err)
			}
			themePlayer = nil
		}
		nativeLoops.Stop(scripts.LoopKindActor, "isao")
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
		choiceBackground, err := standingPoseOver(jonesConversationBase,
			jonesPuppet, jonesPuppetTable, nil, "Jones")
		if err != nil {
			return err
		}
		frame, err := jonesPuppet.ChoiceFrame(choiceBackground, jonesPuppetTable.PanelResource, labels)
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
		if jonesPuppetCode == "threenite" && group == 0 && len(choices) == 3 {
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
		jonesChoicePressActive = false
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
			if jonesPuppetCode == "bigstep" {
				jonesRingStory = 1
			}
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
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
				return fmt.Errorf("register Jones idle loop after dialogue returned status %#x", status)
			}
		}
		if hotelSceneOpenAction != nil && activeSetName == "hotupper" && hotelSceneOpenStep < len(hotelSceneOpenAction.TriggerSteps) {
			step := hotelSceneOpenAction.TriggerSteps[hotelSceneOpenStep]
			if step.Kind == scripts.HotelTriggerDelay {
				hotelSceneOpenStep++
				loopOwner := strings.ToLower(strings.TrimSpace(hotelSceneOpenAction.LoopName + " " + strings.TrimPrefix(hotelSceneOpenAction.LoopScene, "Scene ")))
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindScene, Owner: loopOwner, Callback: hotelSceneOpenAction.LoopCode, Remaining: int32(step.DelayTicks)}); status != 0 {
					return fmt.Errorf("register Jones hallway delay returned status %#x", status)
				}
				if *debug {
					log.Printf("hotel-open delay=%d next=%d actor=jones", step.DelayTicks, hotelSceneOpenStep)
				}
			}
		}
		if *debug {
			log.Printf("actor=jones mousedown-complete actorvalue=%d jonesphase=%d ringstory=%d", jonesActorValue, jonesPhase, jonesRingStory)
		}
		return refreshWorldScene()
	}
	startJonesEventResponse := func(event int32) error {
		if jonesPuppetCode == "threeam" {
			if jonesChoiceGroup == 0 && event == 101 {
				calls, err := scripts.PuppetEventSpeechCalls(jonesPuppetProgram, "threeam", event)
				if err != nil {
					return err
				}
				return startJonesSpeech(calls, 1, false)
			}
			if jonesChoiceGroup == 1 && event == 102 {
				calls, err := scripts.PuppetEventSpeechCalls(jonesPuppetProgram, "threeam", event)
				if err != nil {
					return err
				}
				jonesPuppetCode = "bigstep"
				if jonesRingStory == 1 {
					jonesChoiceGroups = [][]scripts.PuppetChoice{{{Text: "Marriage is a big step.", EventID: 104}, {Text: "Bye, Jackalope.", EventID: 103}}}
				} else {
					jonesChoiceGroups = [][]scripts.PuppetChoice{{{Text: "I'll try to find you a ring.", EventID: 105}, {Text: "Get your own ring, I don't have time.", EventID: 106}, {Text: "Bye, Jackalope.", EventID: 103}}}
				}
				return startJonesSpeech(calls, 0, false)
			}
			return fmt.Errorf("Jones threeam group %d returned event %d", jonesChoiceGroup, event)
		}
		if jonesPuppetCode == "bigstep" {
			if jonesChoiceGroup != 0 {
				return fmt.Errorf("Jones bigstep has unsupported choice group %d", jonesChoiceGroup)
			}
			if event != 103 && event != 104 && event != 105 && event != 106 {
				return fmt.Errorf("Jones bigstep returned event %d", event)
			}
			speechEvent := event
			if speechEvent == 106 {
				speechEvent = 103
			}
			calls, err := scripts.PuppetEventSpeechCalls(jonesPuppetProgram, "bigstep", speechEvent)
			if err != nil {
				return err
			}
			if event == 104 {
				jonesChoiceGroups = [][]scripts.PuppetChoice{{{Text: "I'll try to find you a ring.", EventID: 105}, {Text: "Get your own ring, I don't have time.", EventID: 106}, {Text: "Bye, Jackalope.", EventID: 103}}}
				return startJonesSpeech(calls, 0, false)
			}
			return startJonesSpeech(calls, -1, event == 105)
		}
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
		hotelMorning := activeSetName == "hotupper" && gameDay == 2 && gameClock == 1 && hotelSceneOpenAction != nil
		if gameClock != 3 && !hotelMorning {
			jonesInteractionStage = jonesInteractionIdle
			if *debug {
				log.Printf("puppet=jones blocked clock=%d jonesphase=%d", gameClock, jonesPhase)
			}
			if jonesVisible && activeSetName == "town" {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
					return fmt.Errorf("register Jones idle loop after unsupported dialogue returned status %#x", status)
				}
			}
			return nil
		}
		// **No world actor survives into a conversation.** The puppet fills the 512x264
		// viewport as a foreground close-up, so a bystander left standing behind it appears
		// inside the portrait -- the speaker's giant face with a smaller figure showing
		// through it. The room and the panel stay; the cast does not.
		dialogueActors := make([]render.WorldActorSprite, 0)
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
		// Jones's lines are resolved just below, so the pose falls back to the table's first
		// line rather than the conversation's own. The figure is present either way.
		jonesConversationBase, err = withPuppetPalette(jonesConversationBase, jonesPuppet, "Jones")
		if err != nil {
			return err
		}
		if jonesPhase == 0 {
			code := "threenite"
			if hotelMorning {
				code = "threeam"
				groups, err := scripts.PuppetBevelChoiceGroups(jonesPuppetProgram, code)
				if err != nil {
					return err
				}
				jonesChoiceGroups = groups
			}
			jonesPuppetCode = code
			calls, err := scripts.PuppetSpeechCalls(jonesPuppetProgram, code, scripts.LookupOpcode("puppetclear"))
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
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
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
		nativeLoops.Stop(scripts.LoopKindActor, "jones")
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
		choiceBackground, err := standingPoseOver(marieConversationBase,
			mariePuppet, mariePuppetTable, nil, "Marie")
		if err != nil {
			return err
		}
		frame, err := mariePuppet.ChoiceFrame(choiceBackground, mariePuppetTable.PanelResource, labels)
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
		marieChoicePressActive = false
		marieChoicePressIndex, marieChoiceOutline = -1, -1
		if err := drawMarieChoices(-1); err != nil {
			return err
		}
		marieInteractionStage = marieInteractionPuppetChoices
		return nil
	}
	renderMarieInventory = func() error {
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
		props, err := render.BuildPlayerInventoryProps(inventoryArchive, inventoryOwners, inventoryHidden, handItem, gameDay)
		if err != nil {
			return fmt.Errorf("build avatar inventory props: %w", err)
		}
		for _, prop := range props {
			item := strings.ToLower(prop.Name)
			if _, found := inventoryLargeFrames[item]; found {
				continue
			}
			frame, err := loadInventoryFrame(inventoryArchive, prop.PropName, "large", 0)
			if err != nil {
				return fmt.Errorf("load %s large inventory frame: %w", prop.PropName, err)
			}
			inventoryLargeFrames[item] = frame
		}
		frame, projected, err := render.CompositeFlatProps(base, props)
		if err != nil {
			return fmt.Errorf("draw avatar inventory props: %w", err)
		}
		frame, err = render.DrawPlayerInventoryCash(frame, playercash)
		if err != nil {
			return fmt.Errorf("draw avatar cash: %w", err)
		}
		inventoryCashRendered, inventoryCashRenderedValid = playercash, true
		avatarIdleActive, avatarGestureActive = false, false
		currentScene, currentPixels = 2, pixels
		currentFrame, stageFrame = frame, frame
		marieInventoryProjected = projected
		if *debug {
			log.Printf("flat=avatar props=%d handitem=%q", len(projected), handItem)
		}
		return nil
	}
	enterInventoryScene := func() error {
		nextPixels, err := loadInventoryScenePixels(stage, currentPixels.Pixels)
		if err != nil {
			return err
		}
		if currentScene != 2 {
			setInventoryLoopsPaused(true)
		}
		currentScene, currentPixels, inventoryMenuActive = 2, nextPixels, true
		return renderMarieInventory()
	}
	openMarieInventory := func() error {
		handFlag = 0
		marieInteractionStage = marieInteractionInventory
		return enterInventoryScene()
	}
	openIsaoInventory = func() error {
		handFlag = 0
		isaoInventoryReturnCode = isaoCurrentCode
		isaoInteractionStage = isaoInteractionInventory
		return enterInventoryScene()
	}
	openTrotterInventory = func() error {
		handFlag = 0
		trotterInteractionStage = trotterInteractionInventory
		return enterInventoryScene()
	}
	openLaurelInventory = func() error {
		handFlag = 0
		laurelInteractionStage = laurelInteractionInventory
		return enterInventoryScene()
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
			nativeLoops.Stop(scripts.LoopKindActor, "marie")
		} else if marieVisible && activeSetName == "town" {
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
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
		// **No world actor survives into a conversation.** The puppet fills the 512x264
		// viewport as a foreground close-up, so a bystander left standing behind it appears
		// inside the portrait -- the speaker's giant face with a smaller figure showing
		// through it. The room and the panel stay; the cast does not.
		dialogueActors := make([]render.WorldActorSprite, 0)
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
		marieConversationBase, err = withPuppetPalette(marieConversationBase, mariePuppet, "Marie")
		if err != nil {
			return err
		}
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
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
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
		nativeLoops.Stop(scripts.LoopKindActor, "marie")
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
		nativeLoops.Stop(scripts.LoopKindActor, "jones")
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
			nativeLoops.Stop(scripts.LoopKindActor, "help")
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
	// A script conversation: the open puppet, its base, the line playing and
	// the choice panel, driven by the GameHost through scriptPresenter.
	var scriptTask *scripts.ScriptTask
	var convPuppet *render.Puppet
	var convTable assets.PuppetSpeechTable
	var convName string
	var convBase render.IndexedFrame
	var convDialogue *engine.PuppetDialogue
	var convSkipped, convChoosing, convHasChoice bool
	var convChoices []scripts.PuppetChoice
	var convChosen int32
	convPressIndex := -1
	convPose := func() (render.IndexedFrame, error) {
		return standingPoseOver(convBase, convPuppet, convTable, nil, convName)
	}
	drawConvChoices := func(outline int) error {
		labels := make([]string, len(convChoices))
		for index, choice := range convChoices {
			labels[index] = choice.Text
		}
		pose, err := convPose()
		if err != nil {
			return err
		}
		frame, err := convPuppet.ChoiceFrame(pose, convTable.PanelResource, labels)
		if err != nil {
			return fmt.Errorf("render %s choice panel: %w", convName, err)
		}
		if outline >= 0 {
			if frame, err = render.DrawNativePuppetChoiceBevel(frame, outline); err != nil {
				return fmt.Errorf("render %s choice bevel: %w", convName, err)
			}
		}
		currentFrame, stageFrame = frame, frame
		return nil
	}
	scriptHost.Env.Presenter = &scriptPresenter{
		open: func(file string) error {
			puppet, err := render.OpenPuppet(workspace, file)
			if err != nil {
				return err
			}
			table, err := workspace.OpenPuppetSpeechTable(file)
			if err != nil {
				_ = puppet.Close()
				return err
			}
			// No world actor survives into a conversation (see the Trotter base).
			background, _, err := compositeWorld(backgroundFrame, worldPoint, nil)
			if err != nil {
				_ = puppet.Close()
				return err
			}
			panel, err := render.StageFrame(stage, currentPixels.Pixels)
			if err != nil {
				_ = puppet.Close()
				return err
			}
			base, err := composeMainPanel(background, panel)
			if err != nil {
				_ = puppet.Close()
				return err
			}
			if base, err = withPuppetPalette(base, puppet, file); err != nil {
				_ = puppet.Close()
				return err
			}
			convPuppet, convTable, convName, convBase = puppet, table, file, base
			convChoosing, convHasChoice, convSkipped = false, false, false
			if *debug {
				log.Printf("script-puppet open=%s lines=%d", file, len(table.Entries))
			}
			return nil
		},
		close: func() error {
			if convDialogue != nil {
				if err := convDialogue.Close(); err != nil {
					return err
				}
				convDialogue = nil
			}
			if convPuppet != nil {
				if err := convPuppet.Close(); err != nil {
					return err
				}
			}
			convPuppet, convChoosing = nil, false
			return nil
		},
		speak: func(line string) (bool, error) {
			found := false
			for _, entry := range convTable.Entries {
				if strings.EqualFold(entry.Name, line) {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
			if convDialogue != nil {
				if err := convDialogue.Close(); err != nil {
					return false, err
				}
			}
			dialogue, err := engine.NewPuppetDialogue(convPuppet, convTable.Entries, []string{line}, audioContext)
			if err != nil {
				return false, err
			}
			frame, err := dialogue.Start(convBase, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
			if err != nil {
				return false, err
			}
			convDialogue, convSkipped, convChoosing = dialogue, false, false
			currentFrame, stageFrame = frame, frame
			if *debug {
				log.Printf("script-puppet speak=%s", line)
			}
			return true, nil
		},
		speaking: func() bool { return convDialogue != nil && convDialogue.Active() },
		skipped:  func() bool { return convSkipped },
		choose: func(choices []scripts.PuppetChoice) error {
			convChoices, convChoosing, convHasChoice, convPressIndex = choices, true, false, -1
			if *debug {
				labels := make([]string, len(choices))
				for index, choice := range choices {
					labels[index] = fmt.Sprintf("%s=%d", choice.Text, choice.EventID)
				}
				log.Printf("script-puppet choices=%q", labels)
			}
			return drawConvChoices(-1)
		},
		chosen: func() (int32, bool) {
			if !convHasChoice {
				return 0, false
			}
			convHasChoice = false
			return convChosen, true
		},
		show: func(layer string) error {
			if layer == "puppet" && convPuppet != nil {
				frame, err := convPose()
				if err != nil {
					return err
				}
				currentFrame, stageFrame = frame, frame
				return nil
			}
			return refreshWorldScene()
		},
		cursor: func(name string) error { return nil },
	}
	scriptHost.Env.PuppetFile = workspace.OpenPuppetFile
	scriptHost.Env.Program = scriptProgram
	scriptHost.Env.Shop = func(name string) (string, uint32, bool) {
		// INVEN.PRP is the inventory shop the NEW.FLT boot opens for the
		// whole game; its shop script is resource 0 +0x924.
		if !strings.EqualFold(name, "inven") {
			return "", 0, false
		}
		data, err := scriptResource("DATA/INVEN.PRP", 0)
		if err != nil || len(data) < 0x928 {
			return "", 0, false
		}
		return "DATA/INVEN.PRP", binary.LittleEndian.Uint32(data[0x924:0x928]), true
	}
	scriptHost.Env.PropDegree = func(name string) (int16, bool) {
		return inventoryDegrees[strings.ToLower(name)], true
	}
	scriptHost.Env.Ticks = func() uint32 { return scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) }
	scriptHost.Env.PlayerHeading = func() int16 {
		degree, _ := render.NativeCurrentDegree(worldPoint[2])
		return degree
	}
	scriptHost.Env.Vector = func(heading, length int16) (int16, int16) {
		vector, _ := render.NativeDirectionVector(heading, length)
		return vector[0], vector[1]
	}
	scriptHost.Env.CurrentFlat = func() string {
		return strings.ToLower(string(stage.Scenes[currentScene].Name[1:]))
	}
	scriptHost.Env.Fallback = &mainScriptFallback{tiphat: func() error {
		return startAvatarTiphat(nil)
	}}
	// startScriptTask runs an event that may block (a conversation) as a
	// task the update loop resumes.
	startScriptTask := func(event scripts.ActorEvent) error {
		if scriptTask != nil {
			return nil
		}
		task := scripts.StartScriptTask(event.Actor+" "+event.Message, func(task *scripts.ScriptTask) error {
			scriptHost.SetTask(task)
			return deliverScriptEvent(event)
		})
		if done, err := task.Poll(); done {
			scriptHost.SetTask(nil)
			return err
		}
		scriptTask = task
		return nil
	}
	schedulerPasses, schedulerWindow := 0, scripts.NativeTickMilliseconds()
	// The native pump (FUN_00406920) finishes each render by waiting until the
	// timer FUN_0042B700, which counts 60 units a second, has advanced by the
	// boot script's framerate(3). Walk jobs, loops and projection therefore run
	// 20 times a second, not once per host update.
	const nativePumpInterval uint32 = 3
	nextNativePump := uint32(0)
	nativePumpDue := func() bool {
		now := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
		if int32(now-nextNativePump) < 0 {
			return false
		}
		nextNativePump = now + nativePumpInterval
		return true
	}
	runNativeScheduler := func(visualEffectPump bool) (bool, error) {
		displayChanged := false
		if *debug && *debugLoops {
			schedulerPasses++
			if now := scripts.NativeTickMilliseconds(); now-schedulerWindow >= 1000 {
				log.Printf("scheduler passes=%d window-ms=%d", schedulerPasses, now-schedulerWindow)
				schedulerPasses, schedulerWindow = 0, now
			}
		}
		scriptsPaused := scriptTask != nil && !scriptHost.PassRequested()
		if !visualEffectPump && !scriptsPaused {
			// FUN_0040F4E0 runs the walk and turn jobs before the loops.
			events, moved := scriptHost.Pass()
			displayChanged = displayChanged || moved && currentScene == 0
			for _, event := range events {
				if err := deliverScriptEvent(event); err != nil {
					return false, err
				}
			}
		}
		status, err := nativeLoops.PassWhere(func(loop scripts.ScriptLoop) (uint16, error) {
			if event, isActor := scripts.LoopEvent(loop); isActor && scriptManaged[strings.ToLower(loop.Owner)] {
				displayChanged = displayChanged || currentScene == 0
				return 0, deliverScriptEvent(event)
			}
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
				if *debug && *debugLoops {
					log.Printf("actor=leroy pose=%s next=%s ticks=%d heading=%d", step.Pose, step.Callback, step.Remaining, actorHeadings["leroy"])
				}
			case "nightfxs", "dayfxs":
				cue, found := scripts.NativeSoundCue{}, false
				if loop.Callback == "nightfxs" {
					cue, found = scripts.NightWildlifeCue(currentThemeName, &nativeRandom)
				} else {
					cue, found = scripts.DayWildlifeCue(currentThemeName, &nativeRandom)
				}
				if found {
					played, err := themeBank.PlayAtVolume(audioContext, cue.Name, cue.Volume)
					if err != nil {
						return 0, fmt.Errorf("play NITE wildlife cue %s: %w", cue.Name, err)
					}
					if *debug && *debugLoops {
						log.Printf("ambient-cue=%s volume=%d played=%t", cue.Name, cue.Volume, played)
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
				if *debug && *debugLoops {
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
				if *debug && *debugLoops {
					log.Printf("actor=help pose=%s next=%s ticks=%d attention=%d", step.Pose, step.Callback, step.Remaining, helpAttention)
				}
			case "marieidle":
				name := strings.TrimSuffix(loop.Callback, "idle")
				star, position, moving, visible := "", [3]int16{}, false, false
				interval := int32(17)
				switch name {
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
					marieTurnActive = actorHeadings[name] != actorTurnTargets[name]
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
					marieWalk, marieWalkTarget, actorPoses[name] = &walk, step.Star, "walk"
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
				if *debug && *debugLoops {
					log.Printf("actor=jones idle=turn-check point=%v ticks=%d turn=%t", jonesPosition, loop.Remaining, jonesTurnActive)
				}
			case "trigger":
				if hotelSceneOpenAction == nil || activeSetName != "hotupper" {
					hotelEventsLocked = false
					return 0, nil
				}
				for hotelSceneOpenStep < len(hotelSceneOpenAction.TriggerSteps) {
					step := hotelSceneOpenAction.TriggerSteps[hotelSceneOpenStep]
					switch step.Kind {
					case scripts.HotelTriggerSetPhase:
						gamePhase, phase = step.Phase, int(step.Phase)
					case scripts.HotelTriggerSetLockEvents:
						hotelEventsLocked = step.LockEvents
					case scripts.HotelTriggerRunPuppet:
						hotelSceneOpenStep++
						if gameDay == 2 && strings.EqualFold(step.Actor, "jones") && strings.EqualFold(step.Puppet, "jones.pup") {
							jonesInteractionStage = jonesInteractionPuppetPending
							if *debug {
								log.Printf("hotel-open runpuppet actor=%s puppet=%s phase=%d", step.Actor, step.Puppet, gamePhase)
							}
							return 0, nil
						}
						if *debug {
							log.Printf("hotel-open continuation=%d kind=%d actor=%s puppet=%s pending=true", hotelSceneOpenStep, step.Kind, step.Actor, step.Puppet)
						}
						return 0, nil
					case scripts.HotelTriggerDelay:
						hotelSceneOpenStep++
						loop.Remaining = int32(step.DelayTicks)
						if *debug {
							log.Printf("hotel-open delay=%d next=%d", step.DelayTicks, hotelSceneOpenStep)
						}
						return nativeLoops.Register(loop), nil
					case scripts.HotelTriggerMoveActor:
						position, found, err := activeSet.ResolveLocation(step.Destination)
						if err != nil || !found {
							return 0, fmt.Errorf("resolve hotel actor destination %q: found=%t err=%v", step.Destination, found, err)
						}
						if !strings.EqualFold(step.Actor, "jones") || !hotelJonesVisible {
							return 0, fmt.Errorf("hotel trigger cannot move actor %q", step.Actor)
						}
						heading := render.NativeActorHeadingToPoint(hotelJonesPosition, position)
						walk := scripts.NewNativeActorWalkJob(hotelJonesPosition, position, heading, jonesWalkRate)
						hotelJonesWalk, actorHeadings["jones"], actorPoses["jones"] = &walk, heading, "walk"
						hotelSceneOpenStep++
						hotelSceneOpenAction, hotelEventsLocked = nil, false
						displayChanged = true
						if *debug {
							log.Printf("hotel-open actor=jones walk from=%v to=%v heading=%d", hotelJonesPosition, position, heading)
						}
						return 0, nil
					default:
						if *debug {
							log.Printf("hotel-open continuation=%d kind=%d pending=true phase=%d lockevents=%t", hotelSceneOpenStep, step.Kind, gamePhase, hotelEventsLocked)
						}
						return 0, nil
					}
					hotelSceneOpenStep++
				}
				return 0, nil
			case "laurelidle":
				loop.Remaining = 18
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
				if *debug && *debugLoops {
					log.Printf("actor=isao pose=%s heading=%d next=%s ticks=%d", actorPoses["isao"], actorHeadings["isao"], loop.Callback, loop.Remaining)
				}
			default:
				return 0, fmt.Errorf("unknown native loop callback %q", loop.Callback)
			}
			return nativeLoops.Register(loop), nil
		}, func(loop scripts.ScriptLoop) bool {
			if scriptsPaused && loop.Kind == scripts.LoopKindActor && scriptManaged[strings.ToLower(loop.Owner)] {
				return false
			}
			return visualEffectPump || loop.Callback != "nightfxs" && loop.Callback != "dayfxs"
		})
		if err != nil {
			return false, err
		}
		if status != 0 {
			return false, fmt.Errorf("native scene scheduler returned status %#x", status)
		}
		if !visualEffectPump && actorTurnActive {
			actorHeadings["leroy"] = scripts.NativeTurnStep(actorHeadings["leroy"], actorTurnTargets["leroy"], leroyTurnRate)
			actorTurnActive = actorHeadings["leroy"] != actorTurnTargets["leroy"]
			displayChanged = displayChanged || currentScene == 0
			if *debug && *debugLoops {
				log.Printf("actor=leroy turn heading=%d target=%d active=%t", actorHeadings["leroy"], actorTurnTargets["leroy"], actorTurnActive)
			}
			if !actorTurnActive && leroyInteractionStage == leroyInteractionFacing {
				leroyInteractionStage = leroyInteractionPuppetPending
				if *debug {
					log.Printf("actor=leroy interaction=puppet pending file=PUPPETS/LEROY.PUP")
				}
			}
		}
		if !visualEffectPump && helpTurnActive {
			actorHeadings["help"] = scripts.NativeTurnStep(actorHeadings["help"], actorTurnTargets["help"], helpTurnRate)
			helpTurnActive = actorHeadings["help"] != actorTurnTargets["help"]
			displayChanged = displayChanged || currentScene == 0
			if *debug && *debugLoops {
				log.Printf("actor=help turn heading=%d target=%d active=%t", actorHeadings["help"], actorTurnTargets["help"], helpTurnActive)
			}
		}
		if !visualEffectPump && jonesTurnActive {
			actorHeadings["jones"] = scripts.NativeTurnStep(actorHeadings["jones"], actorTurnTargets["jones"], jonesTurnRate)
			jonesTurnActive = actorHeadings["jones"] != actorTurnTargets["jones"]
			displayChanged = displayChanged || currentScene == 0
			if *debug && *debugLoops {
				log.Printf("actor=jones turn heading=%d target=%d active=%t", actorHeadings["jones"], actorTurnTargets["jones"], jonesTurnActive)
			}
		}
		if !visualEffectPump && marieTurnActive {
			actorHeadings["marie"] = scripts.NativeTurnStep(actorHeadings["marie"], actorTurnTargets["marie"], jonesTurnRate)
			marieTurnActive = actorHeadings["marie"] != actorTurnTargets["marie"]
			displayChanged = displayChanged || currentScene == 0
		}
		if !visualEffectPump && marieInteractionStage == marieInteractionFacing && !marieTurnActive {
			marieInteractionStage = marieInteractionPuppetPending
		}
		if !visualEffectPump && jonesInteractionStage == jonesInteractionFacing && !jonesTurnActive {
			jonesInteractionStage = jonesInteractionPuppetPending
		}
		if !visualEffectPump && helpInteractionStage == helpInteractionFacing && !helpTurnActive {
			helpInteractionStage = helpInteractionPuppetPending
		}
		if leroyWalk != nil {
			previousPosition, previousHeading := leroyPosition, actorHeadings["leroy"]
			var walking bool
			leroyPosition, actorHeadings["leroy"], walking = leroyWalk.Pass(leroyPosition, actorHeadings["leroy"], leroyTurnRate)
			displayChanged = displayChanged || leroyPosition != previousPosition || actorHeadings["leroy"] != previousHeading
			if *debug && (*debugLoops || !walking) && (leroyPosition != previousPosition || actorHeadings["leroy"] != previousHeading) {
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
			if *debug && (*debugLoops || !walking) && (helpActorPosition != previousPosition || actorHeadings["help"] != previousHeading) {
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
			if *debug && (*debugLoops || !walking) && (jonesPosition != previousPosition || actorHeadings["jones"] != previousHeading) {
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
				} else if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "jones", Callback: "jonesidle", Remaining: 17}); status != 0 {
					return false, fmt.Errorf("register Jones idle loop returned status %#x", status)
				}
				displayChanged = true
				if *debug {
					log.Printf("actor=jones endwalk point=%v pose=stand idle=jonesidle", jonesPosition)
				}
			}
		}
		if hotelJonesWalk != nil && activeSetName == "hotupper" {
			previousPosition, previousHeading := hotelJonesPosition, actorHeadings["jones"]
			var walking bool
			hotelJonesPosition, actorHeadings["jones"], walking = hotelJonesWalk.Pass(hotelJonesPosition, actorHeadings["jones"], jonesTurnRate)
			displayChanged = displayChanged || hotelJonesPosition != previousPosition || actorHeadings["jones"] != previousHeading
			if !walking {
				hotelJonesWalk = nil
				actorPoses["jones"] = "stand"
				displayChanged = true
				if *debug {
					log.Printf("hotel-open actor=jones endwalk point=%v pose=stand", hotelJonesPosition)
				}
			}
		}
		if trotterWalk != nil {
			now := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			if now >= trotterWalkNext {
				var walking bool
				trotterPosition, actorHeadings["trotter"], walking = trotterWalk.Pass(trotterPosition, actorHeadings["trotter"], trotterTurnSpeed)
				trotterWalkNext, displayChanged = now+nativeAvatarFrameInterval, true
				if actorHeadings["trotter"] == trotterWalk.Snapshot().Heading {
					if actorPoses["trotter"] == "walk" {
						trotterFrameIndex = (trotterFrameIndex + 1) % 16
					} else {
						actorPoses["trotter"], trotterFrameIndex = "walk", 0
					}
				}
				if !walking {
					trotterWalk, trotterVisible, actorPoses["trotter"] = nil, false, "stand"
					trotterFrameIndex = 0
					nativeLoops.Stop(scripts.LoopKindActor, "trotter")
				}
				if *debug && (*debugLoops || !walking) {
					log.Printf("actor=trotter walk point=%v heading=%d active=%t visible=%t", trotterPosition, actorHeadings["trotter"], walking, trotterVisible)
				}
			}
		}
		if laurelWalk != nil {
			now := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			if now >= laurelWalkNext {
				var walking bool
				previousPosition, previousHeading := laurelPosition, actorHeadings["laurel"]
				laurelPosition, actorHeadings["laurel"], walking = laurelWalk.Pass(laurelPosition, actorHeadings["laurel"], 8)
				laurelWalkNext = now + nativeAvatarFrameInterval
				if laurelPosition != previousPosition || actorHeadings["laurel"] != previousHeading {
					laurelFrameIndex = (laurelFrameIndex + 1) % 16
					displayChanged = true
				}
				if !walking {
					laurelWalk, actorPoses["laurel"], laurelFrameIndex = nil, "stand", 0
					displayChanged = true
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
		if !visualEffectPump && helpInteractionStage == helpInteractionPuppetPending {
			if err := startHelpPuppet(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && helpInteractionStage == helpInteractionPuppetSpeaking {
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
		if !visualEffectPump && helpInteractionStage == helpInteractionPuppetDelay && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= helpDelayUntil {
			helpDelayReady = true
			if err := startHelpSpeechStage(); err != nil {
				return false, fmt.Errorf("continue delayed Help speech: %w", err)
			}
			return true, nil
		}
		if !visualEffectPump && displayChanged && helpInteractionStage == helpInteractionPuppetChoices {
			if err := drawHelpChoices(helpChoiceOutline); err != nil {
				return false, fmt.Errorf("refresh Help choice panel: %w", err)
			}
			return true, nil
		}
		if !visualEffectPump && laurelInteractionStage == laurelInteractionChoices && laurelChoiceUntil != 0 && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= laurelChoiceUntil {
			laurelChoiceUntil, laurelChoicePressActive = 0, false
			if err := startLaurelChoice(-2); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && laurelInteractionStage == laurelInteractionSpeaking {
			if laurelDialogue == nil {
				return false, fmt.Errorf("Laurel dialogue state is missing its puppet player")
			}
			frameTick := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			var frame render.IndexedFrame
			var changed bool
			var err error
			if laurelDialogueSkip {
				frame, changed, err = laurelDialogue.Skip()
				laurelDialogueSkip = false
			} else {
				frame, changed, err = laurelDialogue.Update(frameTick)
			}
			if err != nil {
				return false, fmt.Errorf("advance Laurel dialogue: %w", err)
			}
			if changed {
				currentFrame, stageFrame = frame, frame
				if !laurelDialogue.Active() {
					if err := advanceLaurelSpeech(); err != nil {
						return false, fmt.Errorf("finish Laurel dialogue: %w", err)
					}
				}
				return true, nil
			}
			if displayChanged {
				frame, err = laurelDialogue.Frame()
				if err != nil {
					return false, fmt.Errorf("refresh Laurel dialogue frame: %w", err)
				}
				currentFrame, stageFrame = frame, frame
				return true, nil
			}
		}
		if !visualEffectPump && displayChanged && laurelInteractionStage == laurelInteractionChoices {
			if err := drawLaurelChoices(laurelChoiceOutline); err != nil {
				return false, fmt.Errorf("refresh Laurel choice panel: %w", err)
			}
			return true, nil
		}
		if !visualEffectPump && trotterInteractionStage == trotterInteractionPuppetDelay && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= trotterDelayUntil {
			if err := startTrotterSpeechStage(true); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && trotterInteractionStage == trotterInteractionPuppetChoices && trotterChoiceUntil != 0 && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= trotterChoiceUntil {
			trotterChoiceUntil, trotterChoicePressActive = 0, false
			if err := startTrotterEventResponse(-2); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && trotterInteractionStage == trotterInteractionPuppetSpeaking {
			if trotterDialogue == nil {
				return false, fmt.Errorf("Trotter dialogue state is missing its puppet player")
			}
			frameTick := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			var frame render.IndexedFrame
			var changed bool
			var err error
			if trotterDialogueSkip {
				frame, changed, err = trotterDialogue.Skip()
				trotterDialogueSkip = false
			} else {
				frame, changed, err = trotterDialogue.Update(frameTick)
			}
			if err != nil {
				return false, fmt.Errorf("advance Trotter dialogue: %w", err)
			}
			if changed {
				currentFrame, stageFrame = frame, frame
				if !trotterDialogue.Active() {
					if err := finishTrotterDialogue(); err != nil {
						return false, fmt.Errorf("finish Trotter dialogue: %w", err)
					}
				}
				return true, nil
			}
			if displayChanged {
				frame, err = trotterDialogue.Frame()
				if err != nil {
					return false, fmt.Errorf("refresh Trotter dialogue frame: %w", err)
				}
				currentFrame, stageFrame = frame, frame
				return true, nil
			}
		}
		if !visualEffectPump && isaoInteractionStage == isaoInteractionPuppetPending {
			if err := startIsaoConversation(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && displayChanged && trotterInteractionStage == trotterInteractionPuppetChoices {
			if err := drawTrotterChoices(trotterChoiceOutline); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && laurelInteractionStage == laurelInteractionInventoryReturning && laurelInventoryReturnPending {
			laurelInventoryReturnPending = false
			state, err := laurelStoryState()
			if err != nil {
				return false, err
			}
			step, err := laurelController.Resume(state, laurelContinuation)
			if err != nil {
				return false, err
			}
			if err := startLaurelStep(step); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && trotterInteractionStage == trotterInteractionInventoryReturning && trotterInventoryReturnPending {
			trotterInventoryReturnPending = false
			if err := buildTrotterConversationBase(); err != nil {
				return false, err
			}
			state, err := trotterStoryState()
			if err != nil {
				return false, err
			}
			step, err := trotterScriptController.Resume(state, trotterPendingStep.Continuation)
			if err != nil {
				return false, err
			}
			if err := startTrotterStep(step); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && isaoInteractionStage == isaoInteractionPuppetSpeaking {
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
		if !visualEffectPump && displayChanged && isaoInteractionStage == isaoInteractionPuppetChoices {
			if err := drawIsaoChoices(isaoChoiceOutline); err != nil {
				return false, fmt.Errorf("refresh Isao choices: %w", err)
			}
			return true, nil
		}
		if !visualEffectPump && isaoInteractionStage == isaoInteractionPuppetDelay && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= isaoDelayUntil {
			if isaoKeyMoviePending {
				isaoKeyMoviePending, isaoKeyMovieActive = false, true
				pendingSpotMovie = "MOVIES/KEYS.MOV"
				if *debug {
					log.Printf("actor=isao movie=KEYS.MOV after-frame-delay=60")
				}
				return true, nil
			}
			if err := startIsaoConversation(); err != nil {
				return false, fmt.Errorf("start Isao follow-up: %w", err)
			}
			return true, nil
		}
		if !visualEffectPump && jonesInteractionStage == jonesInteractionPuppetPending {
			if err := startJonesConversation(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && jonesInteractionStage == jonesInteractionPuppetSpeaking {
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
		if !visualEffectPump && displayChanged && jonesInteractionStage == jonesInteractionPuppetChoices {
			if err := showJonesChoices(jonesChoiceGroup); err != nil {
				return false, fmt.Errorf("refresh Jones choice panel: %w", err)
			}
			return true, nil
		}
		if !visualEffectPump && marieInteractionStage == marieInteractionPuppetPending {
			if err := startMarieConversation(); err != nil {
				return false, err
			}
			return true, nil
		}
		if !visualEffectPump && marieInteractionStage == marieInteractionPuppetSpeaking {
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
		if !visualEffectPump && displayChanged && marieInteractionStage == marieInteractionPuppetChoices {
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
			if err := drawLeroyChoices(leroyChoiceOutline); err != nil {
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
		movieName, dogSpotMovie := nativeDogSpotMovie(name)
		if !dogSpotMovie {
			movieName = name
		}
		if !spotMovieStarting && dogSpotMovie {
			pendingSpotMovie = movieName
			if *debug {
				log.Printf("movie=%s routed=spotmovie", movieName)
			}
			return nil
		}
		if currentScene == 0 && !spotMovieStarting {
			if err := refreshWorldScene(); err != nil {
				return fmt.Errorf("refresh scene before movie %s: %w", name, err)
			}
		}
		if err := releaseMovieAudio(); err != nil {
			return fmt.Errorf("stop current movie soundtrack: %w", err)
		}
		if movie != nil {
			if err := movie.Close(); err != nil {
				return fmt.Errorf("close current movie: %w", err)
			}
		}
		movieNames, movieIndex = []string{movieName}, 0
		movieWarningCount, movieWarningSample = []int{0}, []string{""}
		movie, err = render.OpenMovie(workspace, movieName)
		if err != nil {
			return fmt.Errorf("open scene movie %s: %w", movieName, err)
		}
		movieRestorePalette = stage.PaletteRaw
		if activeSet != nil && currentScene == 0 {
			movieRestorePalette = activeSet.Palette()
		}
		movieBase := currentFrame
		moviePaletteBase := movieRestorePalette
		if dogSpotMovie || deathStage == 3 || sceneMovieAfter != nil {
			movieBase, moviePaletteBase = blackFrame, render.BlackPaletteRaw()
			if *debug {
				log.Printf("movie=%s base=blackscreen palette=black", movieName)
			}
		}
		playback, err = render.NewMoviePlayback(movie, movieBase, moviePaletteBase)
		if err != nil {
			return fmt.Errorf("start scene movie %s: %w", movieName, err)
		}
		movieAudio, movieAudioEvents, movieAudioLoop, err = startNativeMovieAudio(audioContext, movie)
		if err != nil {
			return err
		}
		movieAudioEmbedded = nativeMovieHasEmbeddedAudio(movie)
		if *debug {
			audioSource := "events"
			if movieAudioEmbedded {
				audioSource = "embedded"
			} else if movieAudio == nil {
				audioSource = "none"
			}
			log.Printf("movie-audio=%s source=%s events=%d loop=%d playing=%t", movieName, audioSource, movieAudioEvents, movieAudioLoop, movieAudio != nil && movieAudio.IsPlaying())
			log.Printf("movie=%s frames=%d source=SET object-click", name, movie.FrameCount())
		}
		return nil
	}
	var pendingMovement assets.SceneMove
	var pendingPlayerMovement assets.SceneMove
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
	playMovieInputSound := func(argument uint32) error {
		slot := int32(argument)
		if slot == 0 {
			return nil
		}
		var resource uint32
		if slot > 0 {
			resources := movie.EmbeddedSoundResources()
			if int(slot) > len(resources) {
				return fmt.Errorf("movie sound slot %d is outside %d embedded samples", slot, len(resources))
			}
			resource = resources[slot-1]
		} else {
			resource = uint32(-slot)
		}
		data, err := movie.Resource(resource)
		if err != nil {
			return err
		}
		sound, err := audio.DecodeNativeSoundResource(data)
		if err != nil {
			return err
		}
		if audioContext != nil {
			player, err := audio.NewNativePlaylist(audioContext, []audio.NativeSound{sound}, []int{0}, -1)
			if err != nil {
				return err
			}
			if player != nil {
				embeddedMovieAudioTails = append(embeddedMovieAudioTails, player)
			}
		}
		if *debug {
			log.Printf("movie-input-sound argument=%d resource=%d", slot, resource)
		}
		return nil
	}
	updateNativeCursor := func(point uint32) {
		cursor := "arrow"
		if playback != nil {
			if playback.CursorAt(image.Pt(int(int16(point>>16)), int(int16(point)))) {
				cursor = "touch"
			}
			setNativeCursor(cursor)
			return
		}
		if currentScene == 0 {
			if _, found, _ := stage.HitTestSceneHandler(currentScene, point); found {
				cursor = "touch"
			}
			if _, found := render.HitTestWorldActors(projectedActors, image.Pt(int(int16(point>>16)), int(int16(point)))); found {
				cursor = "touch"
			}
			if activeSetName == "town" {
				if _, found := scripts.NiteSceneObjectAction(view.Resource, worldPoint[2], point, gameDay, gameClock); found {
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
			if trotterInteractionStage == trotterInteractionPuppetChoices {
				if _, found := scripts.NativePuppetChoiceAt(point, trotterActiveChoices); found {
					cursor = "touch"
				}
			}
			if laurelInteractionStage == laurelInteractionChoices {
				if _, found := scripts.NativePuppetChoiceAt(point, laurelChoices); found {
					cursor = "touch"
				}
			}
			if activeSetName == "hotlower" && laurelInteractionStage == laurelInteractionIdle {
				if actor, found := render.HitTestWorldActors(projectedActors, image.Pt(int(int16(point>>16)), int(int16(point)))); found && strings.EqualFold(actor, "Laurel") {
					cursor = "touch"
				}
			}
			if (inventoryMenuActive || marieInteractionStage == marieInteractionInventory || isaoInteractionStage == isaoInteractionInventory || trotterInteractionStage == trotterInteractionInventory || laurelInteractionStage == laurelInteractionInventory) && currentScene == 2 {
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
	var readingStage *assets.Stage
	var readingAction scripts.InventoryExamination
	var readingScene int
	defer func() {
		if readingStage != nil {
			_ = readingStage.Close()
		}
	}()
	var actionMovieName string
	startActionMovie := func(name string, duration int, after func(bool) (render.IndexedFrame, bool, error)) error {
		if playback != nil || transition != nil || sceneMovieAfter != nil {
			return fmt.Errorf("scene movie is already active")
		}
		sceneMovieAfter, actionMovieName = after, name
		if duration > 0 {
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, duration)
			if err != nil {
				sceneMovieAfter = nil
				return err
			}
			transition, transitionMode = fade, 7
			return nil
		}
		currentFrame = blackFrame
		return startSceneMovie(name)
	}
	restoreActionFrame := func(frame render.IndexedFrame, duration int) (render.IndexedFrame, bool, error) {
		stageFrame = frame
		if duration > 0 {
			fade, err := render.NewFadeEffect(blackFrame, frame, duration)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode = fade, 8
			currentFrame = fade.CurrentFrame()
		} else {
			currentFrame = frame
		}
		return currentFrame, true, nil
	}
	var pendingSetName, pendingSetScene, pendingSetDirection string
	var transferSetName, transferSetScene, transferSetDirection string
	var flatMouseSession *scripts.FlatMouseSession
	var flatMouseResource uint32
	// flatMouseReturnFlat is the name of the flat that was current when a
	// save/open button was pressed. NEW.FLT resource 24 assigns currentflat() to a
	// local before its gotoflat(1) and then returns with gotoflat(<identifier>),
	// and verified FUN_00411AD0 resolves a string gotoflat argument through a flat
	// name lookup, so the capture is a name and must happen at press time.
	var flatMouseReturnFlat string
	var scoreVolumeBaseFrame render.IndexedFrame
	var scoreVolumeTrack image.Rectangle
	var volumeSliderDragging bool
	composeScoreVolume := func(base render.IndexedFrame, slider scripts.MenuVolumeSlider) (render.IndexedFrame, image.Rectangle, error) {
		frame, projected, err := render.CompositeFlatProps(base, []render.FlatPropSprite{{Name: "slider", PropName: "slider", ViewName: "BASE", Anchor: image.Pt(slider.X, slider.Y), Archive: propArchive}})
		if err != nil {
			return render.IndexedFrame{}, image.Rectangle{}, fmt.Errorf("draw score volume slider: %w", err)
		}
		if len(projected) != 1 {
			return render.IndexedFrame{}, image.Rectangle{}, fmt.Errorf("score volume slider is outside the frame")
		}
		return frame, image.Rect(projected[0].Bounds.Min.X, scoreMenuVolumeTop, projected[0].Bounds.Max.X, scoreMenuVolumeBottom+1), nil
	}
	setScoreMenuVolume := func(level int) (scripts.MenuVolumeSlider, bool, error) {
		slider, status, err := scripts.NativeMenuVolume(level, audio.SetWaveVolume)
		if err != nil {
			return scripts.MenuVolumeSlider{}, false, err
		}
		if status != 0 {
			return scripts.MenuVolumeSlider{}, false, fmt.Errorf("menu volume returned status %#x", status)
		}
		if stage == mainStage && currentScene == 3 && transition == nil && scoreVolumeBaseFrame.Width > 0 {
			frame, track, err := composeScoreVolume(scoreVolumeBaseFrame, slider)
			if err != nil {
				return scripts.MenuVolumeSlider{}, false, err
			}
			currentFrame, stageFrame, scoreVolumeTrack = frame, frame, track
		}
		return slider, true, nil
	}
	cloneStringMap := func(source map[string]string) map[string]string {
		result := make(map[string]string, len(source))
		for key, value := range source {
			result[key] = value
		}
		return result
	}
	cloneBoolMap := func(source map[string]bool) map[string]bool {
		result := make(map[string]bool, len(source))
		for key, value := range source {
			result[key] = value
		}
		return result
	}
	cloneStringMapTo := func(destination, source map[string]string) {
		for key := range destination {
			delete(destination, key)
		}
		for key, value := range source {
			destination[key] = value
		}
	}
	cloneBoolMapTo := func(destination, source map[string]bool) {
		for key := range destination {
			delete(destination, key)
		}
		for key, value := range source {
			destination[key] = value
		}
	}
	captureGameProgress := func() save.GameProgress {
		positions := map[string][3]int16{"leroy": leroyPosition, "help": helpActorPosition, "jones": jonesPosition, "buick": buickPosition, "marie": mariePosition, "isao": isaoPosition, "trotter": trotterPosition, "laurel": laurelPosition, "bone": boneWorldProp.Position}
		progress := save.GameProgress{Version: 1, Day: gameDay, Clock: gameClock, Phase: int16(phase), GamePhase: gamePhase, SetName: activeSetName, ViewName: string(view.Name[1:]), TownReturnScene: townReturnScene, Point: worldPoint, PlayerCash: playercash, InventoryOwners: cloneStringMap(inventoryOwners), InventoryHidden: cloneBoolMap(inventoryHidden), HandItem: handItem, HandFlag: handFlag, BoneOwner: boneOwner, BoneInInventory: boneInInventory, BoneWorldVisible: boneWorldProp.Visible, DogVisible: dogVisibleState, ActorPoses: cloneStringMap(actorPoses), ActorHeadings: actorHeadings, ActorPositions: positions, StoryValues: map[string]int32{"isaoActorValue": isaoActorValue, "isaoGiftCounter": isaoGiftCounter, "isaoPhase": int32(isaoPhase), "trotterPhase": int32(trotterPhase), "helpActorValue": helpActorValue, "helpPhase": int32(helpPhase), "jonesActorValue": jonesActorValue, "jonesPhase": int32(jonesPhase), "marieActorValue": marieActorValue, "mariePhase": int32(mariePhase), "laurelPhase": int32(laurelPhase), "oonaActorValue": oonaActorValue}, StoryFlags: map[string]bool{"isaoVisible": isaoVisible, "isaoBouncer": isaoBouncer, "isaoDirGo": isaoDirGo, "helpVisible": helpVisible, "marieVisible": marieVisible, "jonesVisible": jonesVisible, "marieFlag1": marieFlag1, "marieFlag2": marieFlag2, "marieFlag3": marieFlag3}}
		progress.StoryValues["trotterGiftCounter"] = trotterGiftCounter
		progress.StoryValues["jonesRingStory"] = int32(jonesRingStory)
		progress.StoryValues["trotterActorValue"] = trotterActorValue
		progress.StoryValues["bloodPhase"], progress.StoryValues["docPhase"], progress.StoryValues["fightOn"] = int32(bloodPhase), int32(docPhase), int32(fightOn)
		progress.StoryValues["fearPhase"] = int32(fearPhase)
		progress.StoryValues["laurelGood"], progress.StoryValues["laurelActorValue"], progress.StoryValues["laurelCounter"], progress.StoryValues["hankerchiefDegree"], progress.StoryValues["oonakidstory"] = int32(laurelGood), laurelActorValue, laurelCounter, int32(hankerchiefDegree), oonakidstory
		progress.StoryStrings = map[string]string{"hotelSavedScene": hotelSavedScene, "hotelSavedDirection": hotelSavedDirection, "trotterActorSet": trotterActorSet, "trotterStar": trotterStar}
		progress.InventoryDegrees = make(map[string]int16, len(inventoryDegrees))
		for item, degree := range inventoryDegrees {
			progress.InventoryDegrees[item] = degree
		}
		progress.StoryValues["trotterScale"], progress.StoryValues["trotterSpeed"], progress.StoryValues["trotterTurnSpeed"], progress.StoryValues["trotterZClip"] = int32(trotterScale), int32(trotterSpeed), int32(trotterTurnSpeed), int32(trotterZClip)
		progress.StoryFlags["trotterVisible"], progress.StoryFlags["laurelVisible"] = trotterVisible, laurelVisible
		loops := nativeLoops.Snapshot()
		progress.ScriptLoops, progress.NativeLoopKinds = &loops, true
		managed := make([]string, 0, len(scriptManaged))
		for name := range scriptManaged {
			managed = append(managed, name)
		}
		progress.ScriptActors = scriptActors.Snapshot(managed)
		if globals, err := scriptInterpreter.SnapshotGlobals(); err != nil {
			log.Printf("save: script globals unavailable: %v", err)
		} else {
			progress.ScriptGlobals = globals
		}
		if trotterWalk != nil {
			walk := trotterWalk.Snapshot()
			progress.TrotterWalk, progress.TrotterWalkFrame = &walk, trotterFrameIndex
			now := scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())
			if trotterWalkNext > now {
				progress.TrotterWalkRemaining = trotterWalkNext - now
			}
		}
		progress.ActorHeadings = make(map[string]int16, len(actorHeadings))
		for name, heading := range actorHeadings {
			progress.ActorHeadings[name] = heading
		}
		return progress
	}
	initialProgress := captureGameProgress()
	initialProgress.ScriptLoops = nil
	saveGameProgress := func(gameName string) error {
		savePath, err := save.GameProgressPath(workspace.WorkDir, gameName)
		if err != nil {
			return err
		}
		progress := captureGameProgress()
		if err := save.SaveGameProgress(savePath, progress); err != nil {
			return err
		}
		if *debug {
			log.Printf("save=written path=%s day=%d clock=%d set=%s view=%s items=%d cash=%d", savePath, gameDay, gameClock, activeSetName, view.Name[1:], len(inventoryOwners), playercash)
		}
		return nil
	}
	applyGameProgress := func(progress save.GameProgress, savePath string) error {
		gameDay, gameClock, phase, gamePhase, playercash = progress.Day, progress.Clock, int(progress.Phase), progress.GamePhase, progress.PlayerCash
		cloneStringMapTo(inventoryOwners, progress.InventoryOwners)
		cloneBoolMapTo(inventoryHidden, progress.InventoryHidden)
		handItem, handFlag = progress.HandItem, progress.HandFlag
		boneOwner, boneInInventory, boneWorldProp.Visible = progress.BoneOwner, progress.BoneInInventory, progress.BoneWorldVisible
		boneWorldProp.Position = progress.ActorPositions["bone"]
		if boneInInventory {
			boneInventoryFrame, boneWorldProp.View = inventoryLargeFrames["bone"], boneLargeView
		} else {
			boneWorldProp.View = boneSmallView
		}
		dogVisibleState = progress.DogVisible
		if value, found := progress.StoryValues["isaoActorValue"]; found {
			isaoActorValue = value
		}
		if value, found := progress.StoryValues["isaoGiftCounter"]; found {
			isaoGiftCounter = value
		}
		if value, found := progress.StoryValues["isaoPhase"]; found {
			isaoPhase = int16(value)
		}
		if value, found := progress.StoryValues["trotterPhase"]; found {
			trotterPhase = int16(value)
		}
		trotterGiftCounter = progress.StoryValues["trotterGiftCounter"]
		trotterActorValue = progress.StoryValues["trotterActorValue"]
		bloodPhase, docPhase, fightOn = int16(progress.StoryValues["bloodPhase"]), int16(progress.StoryValues["docPhase"]), int16(progress.StoryValues["fightOn"])
		fearPhase = int16(progress.StoryValues["fearPhase"])
		laurelGood, laurelActorValue, laurelCounter, hankerchiefDegree, oonakidstory = int16(progress.StoryValues["laurelGood"]), progress.StoryValues["laurelActorValue"], progress.StoryValues["laurelCounter"], int16(progress.StoryValues["hankerchiefDegree"]), progress.StoryValues["oonakidstory"]
		laurelPosition = progress.ActorPositions["laurel"]
		laurelVisible = progress.StoryFlags["laurelVisible"]
		if value, found := progress.StoryStrings["hotelSavedScene"]; found {
			hotelSavedScene = value
		}
		if value, found := progress.StoryStrings["hotelSavedDirection"]; found {
			hotelSavedDirection = value
		}
		for item := range inventoryDegrees {
			delete(inventoryDegrees, item)
		}
		for item, degree := range progress.InventoryDegrees {
			inventoryDegrees[item] = degree
		}
		if visible, found := progress.StoryFlags["trotterVisible"]; found {
			trotterVisible = visible
		}
		if value, found := progress.StoryValues["helpActorValue"]; found {
			helpActorValue = value
		}
		if value, found := progress.StoryValues["helpPhase"]; found {
			helpPhase = int16(value)
		}
		if value, found := progress.StoryValues["jonesActorValue"]; found {
			jonesActorValue = value
		}
		if value, found := progress.StoryValues["jonesPhase"]; found {
			jonesPhase = int16(value)
		}
		jonesRingStory = int16(progress.StoryValues["jonesRingStory"])
		if value, found := progress.StoryValues["marieActorValue"]; found {
			marieActorValue = value
		}
		if value, found := progress.StoryValues["mariePhase"]; found {
			mariePhase = int16(value)
		}
		if value, found := progress.StoryValues["laurelPhase"]; found {
			laurelPhase = int16(value)
		}
		if value, found := progress.StoryValues["oonaActorValue"]; found {
			oonaActorValue = value
		}
		townReturnScene = progress.TownReturnScene
		currentScene, currentPixels = 0, pixels
		direction := "north"
		switch progress.Point[2] {
		case assets.SetDirectionSouth:
			direction = "south"
		case assets.SetDirectionEast:
			direction = "east"
		case assets.SetDirectionWest:
			direction = "west"
		}
		if _, err := switchSpecialSet(progress.SetName, progress.ViewName, direction); err != nil {
			return err
		}
		cloneStringMapTo(actorPoses, progress.ActorPoses)
		if value, found := progress.StoryStrings["trotterActorSet"]; found {
			trotterActorSet = value
		}
		if value, found := progress.StoryStrings["trotterStar"]; found {
			trotterStar = value
		}
		for name, target := range map[string]*int16{"trotterScale": &trotterScale, "trotterSpeed": &trotterSpeed, "trotterTurnSpeed": &trotterTurnSpeed, "trotterZClip": &trotterZClip} {
			if value, found := progress.StoryValues[name]; found {
				*target = int16(value)
			}
		}
		for name, heading := range progress.ActorHeadings {
			actorHeadings[name] = heading
		}
		for name, target := range map[string]*[3]int16{"leroy": &leroyPosition, "help": &helpActorPosition, "jones": &jonesPosition, "buick": &buickPosition, "marie": &mariePosition, "isao": &isaoPosition, "trotter": &trotterPosition, "bone": &boneWorldProp.Position} {
			if position, found := progress.ActorPositions[name]; found {
				*target = position
			}
		}
		for name, target := range map[string]*bool{"isaoVisible": &isaoVisible, "isaoBouncer": &isaoBouncer, "isaoDirGo": &isaoDirGo, "trotterVisible": &trotterVisible, "helpVisible": &helpVisible, "marieVisible": &marieVisible, "jonesVisible": &jonesVisible, "marieFlag1": &marieFlag1, "marieFlag2": &marieFlag2, "marieFlag3": &marieFlag3} {
			if value, found := progress.StoryFlags[name]; found {
				*target = value
			}
		}
		if gameDay == 1 {
			marieVisible = false
		}
		worldPoint = progress.Point
		trotterWalk, trotterFrameIndex = nil, 0
		if progress.TrotterWalk != nil {
			walk, err := scripts.RestoreNativeActorWalkJob(*progress.TrotterWalk)
			if err != nil {
				return err
			}
			trotterWalk, trotterFrameIndex, trotterWalkNext = walk, progress.TrotterWalkFrame, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())+progress.TrotterWalkRemaining
		}
		if err := scriptInterpreter.RestoreGlobals(progress.ScriptGlobals); err != nil {
			return err
		}
		if err := scriptActors.Restore(progress.ScriptActors); err != nil {
			return err
		}
		if progress.ScriptActors == nil {
			// A save from before script-managed actors (or a new game):
			// have the cast initialize them for the saved day, as initall does.
			if err := runInitActors(); err != nil {
				return err
			}
		}
		if progress.ScriptLoops != nil {
			if !progress.NativeLoopKinds {
				progress.ScriptLoops.MigrateLegacyKinds()
			}
			if err := nativeLoops.Restore(*progress.ScriptLoops); err != nil {
				return err
			}
			setInventoryLoopsPaused(false)
		}
		currentScene, inventoryMenuActive = 0, false
		startAvatarNoFace(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err := refreshWorldScene(); err != nil {
			return err
		}
		if *debug {
			log.Printf("save=loaded path=%s day=%d clock=%d set=%s view=%s items=%d cash=%d", savePath, gameDay, gameClock, activeSetName, view.Name[1:], len(inventoryOwners), playercash)
		}
		return nil
	}
	loadGameProgressFile := func(savePath string) error {
		progress, err := save.LoadGameProgress(savePath)
		if err != nil {
			return err
		}
		return applyGameProgress(progress, savePath)
	}
	loadGameProgress := func(gameName string) error {
		savePath, err := save.GameProgressPath(workspace.WorkDir, gameName)
		if err != nil {
			return err
		}
		return loadGameProgressFile(savePath)
	}
	finishPlayerDeath = func(cause string) error {
		sequence, err := scripts.NewDeathSequence(workspace, cause, &nativeRandom)
		if err != nil {
			return err
		}
		deathSequence, deathStage, deathUntil = sequence, 1, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())+uint32(sequence.DelayFrames)
		avatarIdleActive, avatarGestureActive, avatarTipActive = false, false, false
		if *debug {
			log.Printf("playerdeath=%s movie=%s narration=%v", cause, sequence.Movie, sequence.Narration)
		}
		return nil
	}
	startDeathMovie := func() error {
		nativeLoops = scripts.LoopScheduler{}
		leroyWalk, helpWalk, jonesWalk, marieWalk, trotterWalk = nil, nil, nil, nil, nil
		actorTurnActive, helpTurnActive, jonesTurnActive, marieTurnActive = false, false, false, false
		if themePlayer != nil {
			if err := themePlayer.Close(); err != nil {
				return err
			}
			themePlayer = nil
		}
		if themeBank != nil {
			if err := themeBank.Close(); err != nil {
				return err
			}
			themeBank = nil
		}
		if activeSetOwned && activeSet != nil {
			if err := activeSet.Close(); err != nil {
				return err
			}
		}
		activeSet, activeSetName, activeSetOwned = nil, "", false
		worldActors, projectedActors = nil, nil
		lease, err := stage.AcquireSceneFrameResource(4)
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
		nextPixels, decodeErr := render.DecodeMoviePixels(data, nil)
		if len(nextPixels.Pixels) == 0 {
			return fmt.Errorf("decode death flat: %w", decodeErr)
		}
		deathFrame, err = render.StageFrame(stage, nextPixels.Pixels)
		if err != nil {
			return err
		}
		currentScene, currentPixels, currentFrame, stageFrame, deathStage = 4, nextPixels, blackFrame, deathFrame, 3
		return startSceneMovie(deathSequence.Movie)
	}
	startDeathNarration := func() error {
		var err error
		deathBank, err = audio.OpenSoundBank(workspace, deathSequence.SoundBank)
		if err != nil {
			return err
		}
		tracks, events := make([]audio.NativeSound, len(deathSequence.Narration)), make([]int, len(deathSequence.Narration))
		var duration uint64
		for index, voice := range deathSequence.Narration {
			tracks[index], err = deathBank.Load(voice)
			if err != nil {
				return err
			}
			events[index] = index
			duration += (uint64(len(tracks[index].Samples))*60 + uint64(tracks[index].Format.SampleRate) - 1) / uint64(tracks[index].Format.SampleRate)
		}
		if !*silent {
			deathNarration, err = audio.NewNativePlaylist(audioContext, tracks, events, -1)
			if err != nil {
				return err
			}
		}
		deathUntil, deathStage = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())+uint32(duration), 5
		return nil
	}
	startNewGame := func() error {
		nativeLoops = scripts.LoopScheduler{}
		leroyWalk, helpWalk, jonesWalk, marieWalk, trotterWalk = nil, nil, nil, nil, nil
		leroyInteractionStage, helpInteractionStage, jonesInteractionStage, marieInteractionStage, isaoInteractionStage, trotterInteractionStage = leroyInteractionIdle, helpInteractionIdle, jonesInteractionIdle, marieInteractionIdle, isaoInteractionIdle, trotterInteractionIdle
		laurelInteractionStage, laurelWalk, laurelInventoryReturnPending = laurelInteractionIdle, nil, false
		laurelPendingStep, laurelChoices, laurelContinuation, laurelChoicePressActive = scripts.LaurelStep{}, nil, nil, false
		marieInventoryReturnPending, isaoInventoryReturnPending, trotterInventoryReturnPending, avatarTipActive = false, false, false, false
		marieGiftCounter, deathStage = 0, 0
		if err := applyGameProgress(initialProgress, "NEW.FLT/death/new"); err != nil {
			return err
		}

		if themePlayer != nil {
			if err := themePlayer.Close(); err != nil {
				return err
			}
			themePlayer = nil
		}
		if themeBank != nil {
			if err := themeBank.Close(); err != nil {
				return err
			}
		}
		var err error
		themeBank, err = audio.OpenSoundBank(workspace, "DATA/NIGHT.SND")
		if err != nil {
			return err
		}
		currentTheme, err = themeBank.LoadTheme("town.snd")
		if err != nil {
			return err
		}
		themePlayer, err = currentTheme.Play(audioContext)
		if err != nil {
			return err
		}
		currentThemeName = currentTheme.FirstVoiceName()
		step, found := scripts.DogIdleStep("doright", &nativeRandom)
		if found {
			actorPoses["dog"] = step.Pose
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "dog", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
				return fmt.Errorf("register new-game dog idle: %#x", status)
			}
		}
		if currentThemeName == "nightwind3" {
			nativeLoops.Stop(scripts.LoopKindScene, "scene g14")
			if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindScene, Owner: "scene g14", Callback: "nightfxs", Remaining: 2}); status != 0 {
				return fmt.Errorf("register new-game nightfxs: %#x", status)
			}
		}
		return refreshWorldScene()
	}
	hotelStoryState := func() scripts.HotelStoryState {
		return scripts.HotelStoryState{Day: gameDay, Clock: gameClock, Phase: gamePhase, FearPhase: fearPhase, LaurelPhase: laurelPhase, InventoryOwners: inventoryOwners, SavedScene: hotelSavedScene, SavedDirection: hotelSavedDirection}
	}
	var advanceDay func() (render.IndexedFrame, bool, error)
	advanceDay = func() (render.IndexedFrame, bool, error) {
		route, err := scripts.NativeAdvanceDayRoute(scripts.AdvanceDayState{Day: gameDay, Clock: gameClock, Phase: gamePhase, CurrentSet: activeSetName, InventoryOwners: inventoryOwners})
		if err != nil {
			return render.IndexedFrame{}, false, err
		}
		finish := func(bool) (render.IndexedFrame, bool, error) {
			gameDay, gameClock, gamePhase, phase = route.Day, route.Clock, route.Phase, int(route.Phase)
			dogVisibleState, helpVisible, trotterVisible, trotterPhase = false, false, false, 0
			hotelSavedScene, hotelSavedDirection = route.SavedScene, route.SavedDirection
			townReturnScene = route.TownReturnScene
			if route.FightOn {
				fightOn = 1
			}
			if route.GiveChest {
				inventoryOwners["chest"], inventoryHidden["chest"], handItem = "stranger", false, "chest"
			}
			if themePlayer != nil {
				if err := themePlayer.Close(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				themePlayer = nil
			}
			if themeBank != nil {
				if err := themeBank.Close(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				themeBank = nil
			}
			currentThemeName, nativeLoops = "", scripts.LoopScheduler{}
			startAvatarNoFace(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
			defer func() {
				if err := runInitActors(); err != nil {
					log.Printf("initactors after advanceday: %v", err)
				}
			}()
			sceneName, direction := route.ViewName, route.Direction
			if sceneName == "" {
				name, nativeDirection, err := scripts.NativeSetInitialView(workspace, "DATA/"+strings.ToUpper(route.SetName))
				if err != nil {
					return render.IndexedFrame{}, false, err
				}
				sceneName, direction = name, [...]string{"", "north", "south", "east", "west"}[nativeDirection]
			}
			frame, err := switchSpecialSet(route.SetName, sceneName, direction)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			townReturnScene = route.TownReturnScene
			if *debug {
				log.Printf("advanceday=day:%d clock:%d phase:%d set=%s scene=%s", gameDay, gameClock, gamePhase, activeSetName, view.Name[1:])
			}
			return restoreActionFrame(frame, 30)
		}
		if route.ResetDayOne {
			if err := startNewGame(); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		}
		if route.Movie != "" {
			if err := startActionMovie(route.Movie, 0, finish); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return playback.CurrentFrame(), true, nil
		}
		return finish(false)
	}
	startHotelVoiceStep := func() error {
		for hotelVoiceIndex < len(hotelVoiceSteps) {
			step := hotelVoiceSteps[hotelVoiceIndex]
			hotelVoiceIndex++
			if step.ClearDoor {
				doorOwner = ""
			}
			if step.DoorOwner != "" {
				doorOwner = step.DoorOwner
			}
			if step.DelayFrames > 0 {
				hotelDelayUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + uint32(step.DelayFrames)
				return nil
			}
			if step.Cue == "" {
				continue
			}
			context := audioContext
			if *silent {
				context = nil
			}
			voice, err := engine.NewVoiceOne(hotelVoiceBank, step.Cue, context, nil)
			if err != nil {
				return err
			}
			if err := voice.Start(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())); err != nil {
				return err
			}
			hotelVoice = voice
			if *debug {
				log.Printf("hotel-voice=%s door=%s", step.Cue, doorOwner)
			}
			return nil
		}
		hotelVoiceSteps = nil
		if hotelVoiceBank != nil {
			if err := hotelVoiceBank.Close(); err != nil {
				return err
			}
			hotelVoiceBank = nil
		}
		return nil
	}
	openHotelHotplate := func(action scripts.HotelAction) (render.IndexedFrame, error) {
		if hotelHotplateStage != nil {
			return render.IndexedFrame{}, fmt.Errorf("HOTPLATE stage is already active")
		}
		nextStage, err := workspace.OpenStage(action.StageName)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		if len(nextStage.Scenes) <= 1 {
			_ = nextStage.Close()
			return render.IndexedFrame{}, fmt.Errorf("%s has no native starting flat", action.StageName)
		}
		lease, err := nextStage.AcquireSceneFrameResource(1)
		if err != nil {
			_ = nextStage.Close()
			return render.IndexedFrame{}, err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			_ = nextStage.Close()
			return render.IndexedFrame{}, err
		}
		nextPixels, decodeErr := render.DecodeMoviePixels(data, currentPixels.Pixels)
		if len(nextPixels.Pixels) == 0 {
			_ = nextStage.Close()
			return render.IndexedFrame{}, fmt.Errorf("decode %s flat %s: %w", action.StageName, nextStage.Scenes[1].Name[1:], decodeErr)
		}
		frame, err := render.StageFrame(nextStage, nextPixels.Pixels)
		if err != nil {
			_ = nextStage.Close()
			return render.IndexedFrame{}, err
		}
		var nextBank *audio.SoundBank
		if action.SoundBank != "" {
			nextBank, err = audio.OpenSoundBank(workspace, action.SoundBank)
			if err != nil {
				_ = nextStage.Close()
				return render.IndexedFrame{}, err
			}
		}
		hotelHotplateStage, hotelHotplateBank = nextStage, nextBank
		stage, currentScene, currentPixels = hotelHotplateStage, 1, nextPixels
		currentFrame, stageFrame, inventoryMenuActive = frame, frame, false
		if action.SetPhase {
			gamePhase, phase = action.Phase, int(action.Phase)
		}
		if action.ResetFearPhase {
			fearPhase = 0
		}
		if action.SetLaurelPhase {
			laurelPhase = action.LaurelPhase
		}
		if strings.EqualFold(action.PutDownActor, "laurel") {
			laurelVisible, laurelWalk = false, nil
			nativeLoops.Stop(scripts.LoopKindActor, "laurel")
		}
		for _, setup := range action.ActorSetups {
			if scriptManaged[strings.ToLower(setup.Name)] {
				if err := deliverScriptEvent(scripts.ActorEvent{Actor: setup.Name, Message: fmt.Sprintf("setupactor(%q)", setup.Selector)}); err != nil {
					return render.IndexedFrame{}, err
				}
			} else if strings.EqualFold(setup.Name, "trotter") {
				if err := setupTrotterActor(setup.Selector); err != nil {
					return render.IndexedFrame{}, err
				}
			} else if strings.EqualFold(setup.Name, "marie") {
				star := "town.marie1"
				if setup.Selector == "day2PM" {
					star = "town.mwife3"
				} else if setup.Selector != "day2street" {
					return render.IndexedFrame{}, fmt.Errorf("unsupported Marie hotel setup %q", setup.Selector)
				}
				position, found, err := nightSet.ResolveLocation(star)
				if err != nil || !found {
					return render.IndexedFrame{}, fmt.Errorf("resolve Marie star %q: found=%t err=%v", star, found, err)
				}
				mariePosition, marieStar, marieVisible, marieWalk, marieWalkTarget = position, star, true, nil, ""
				actorPoses["marie"], actorHeadings["marie"] = "stand", 128
				marieTurnActive = false
				nativeLoops.Stop(scripts.LoopKindActor, "marie")
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
					return render.IndexedFrame{}, fmt.Errorf("register Marie hotel setup idle loop: %#x", status)
				}
				if *debug {
					log.Printf("actor=marie setup=%s set=town star=%s point=%v heading=128", setup.Selector, star, position)
				}
			}
		}
		if *debug {
			flatNames := make([]string, len(stage.Scenes))
			for index, scene := range stage.Scenes {
				flatNames[index] = string(scene.Name[1:])
			}
			log.Printf("hotel-stage=%s flat=%s flats=%v sound-bank=%s", action.StageName, stage.Scenes[1].Name[1:], flatNames, action.SoundBank)
		}
		return frame, nil
	}
	runHotelAction := func(action scripts.HotelAction) (render.IndexedFrame, bool, error) {
		if action.Sound != "" && action.Kind != scripts.HotelActionHotplateChoice {
			bank := soundBank
			if hotelHotplateStage != nil && stage == hotelHotplateStage && hotelHotplateBank != nil {
				bank = hotelHotplateBank
			}
			if err := bank.Play(audioContext, action.Sound, 1); err != nil {
				return render.IndexedFrame{}, false, err
			}
		}
		switch action.Kind {
		case scripts.HotelActionDoor:
			if action.GiveItem != "" {
				inventoryOwners[action.GiveItem], inventoryHidden[action.GiveItem] = "stranger", false
				if err := soundBank.Play(audioContext, "inven", 1); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
			if action.DelayFrames > 0 {
				hotelDoorAfterDelay, hotelDelayUntil = action.DoorOwner, scripts.NativeFrameUnits(scripts.NativeTickMilliseconds())+uint32(action.DelayFrames)
			} else {
				doorOwner = action.DoorOwner
			}
		case scripts.HotelActionKnock:
			doorOwner = ""
		case scripts.HotelActionPuppet:
			if !strings.EqualFold(action.Puppet, "PUPPETS/LAUREL.PUP") || gameDay != 2 || gameClock != 1 {
				return render.IndexedFrame{}, false, fmt.Errorf("hotel puppet %q has no active Day2 Laurel route", action.Puppet)
			}
			if action.SetLaurelPhase {
				laurelPhase = action.LaurelPhase
			}
			if err := openLaurelPuppet(); err != nil {
				return render.IndexedFrame{}, false, err
			}
			state, err := laurelStoryState()
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			step, err := laurelController.BeginBreakfast(state, &nativeRandom)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if err := startLaurelStep(step); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		case scripts.HotelActionBreakfast:
			if playback != nil || transition != nil || sceneMovieAfter != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("HOTPLATE transition is already active")
			}
			hotelHotplatePending = action
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, action.FadeOutFrames)
			if err != nil {
				hotelHotplatePending = scripts.HotelAction{}
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode = fade, 9
			return fade.CurrentFrame(), true, nil
		case scripts.HotelActionHotplateChoice:
			item := strings.ToLower(action.AddInventory)
			if item == "" {
				return render.IndexedFrame{}, false, fmt.Errorf("HOTPLATE choice has no inventory item")
			}
			if _, found := inventoryLargeFrames[item]; !found {
				frame, err := loadInventoryFrame(inventoryArchive, action.AddInventory, "large", 0)
				if err != nil {
					return render.IndexedFrame{}, false, err
				}
				inventoryLargeFrames[item] = frame
			}
			frame, changed, err := applyFlatTarget(action.FlatTarget-1, 0, 0, 0)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			inventoryOwners[item], inventoryHidden[item] = "stranger", false
			if action.Sound != "" {
				if err := hotelHotplateBank.Play(audioContext, action.Sound, 1); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
			if *debug {
				log.Printf("hotplate-choice item=%s flat=%d", item, action.FlatTarget)
			}
			return frame, changed, nil
		case scripts.HotelActionHotplatePaper:
			returnFrame := currentFrame
			if err := startActionMovie(action.Movie, action.FadeOutFrames, func(bool) (render.IndexedFrame, bool, error) {
				return restoreActionFrame(returnFrame, action.FadeInFrames)
			}); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		case scripts.HotelActionHotplateSound:
			return currentFrame, action.Sound != "", nil
		case scripts.HotelActionHotplateExit:
			if hotelHotplateStage == nil || stage != hotelHotplateStage || playback != nil || transition != nil {
				return currentFrame, false, nil
			}
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, action.FadeOutFrames)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			hotelHotplatePending, transition, transitionMode = action, fade, 10
			return fade.CurrentFrame(), true, nil
		case scripts.HotelActionFearVoices:
			fearPhase = action.FearPhase
			if hotelVoice != nil {
				return currentFrame, false, nil
			}
			var err error
			hotelVoiceBank, err = audio.OpenSoundBank(workspace, action.SoundBank)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			hotelVoiceSteps, hotelVoiceIndex = action.VoiceSteps, 0
			if err := startHotelVoiceStep(); err != nil {
				return render.IndexedFrame{}, false, err
			}
		case scripts.HotelActionSleep, scripts.HotelActionMovie:
			returnFrame := currentFrame
			if err := startActionMovie(action.Movie, action.FadeOutFrames, func(marker bool) (render.IndexedFrame, bool, error) {
				if action.Kind == scripts.HotelActionSleep && marker {
					return advanceDay()
				}
				return restoreActionFrame(returnFrame, 30)
			}); err != nil {
				return render.IndexedFrame{}, false, err
			}
		default:
			return render.IndexedFrame{}, false, fmt.Errorf("hotel action %d requires its native dialogue or stage", action.Kind)
		}
		return currentFrame, true, nil
	}
	queueHotelTransition := func(action scripts.HotelAction) error {
		pendingHotelActorSetup, pendingHotelActorSelector = action.ActorSetup, action.ActorSelector
		if action.SaveRoomReturn {
			hotelSavedScene, hotelSavedDirection = string(view.Name[1:]), "east"
		}
		sceneName, direction := action.ViewName, action.Direction
		if sceneName == "" {
			name, nativeDirection, err := scripts.NativeSetInitialView(workspace, "DATA/"+strings.ToUpper(action.SetName))
			if err != nil {
				return err
			}
			sceneName, direction = name, [...]string{"", "north", "south", "east", "west"}[nativeDirection]
		}
		if action.Movie == "" {
			pendingSetName, pendingSetScene, pendingSetDirection, doorOwner = action.SetName, sceneName, direction, ""
			return nil
		}
		return startActionMovie(action.Movie, action.FadeOutFrames, func(bool) (render.IndexedFrame, bool, error) {
			frame, err := switchSpecialSet(action.SetName, sceneName, direction)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			return restoreActionFrame(frame, 30)
		})
	}
	renderReadingPage := func() (render.IndexedFrame, error) {
		lease, err := readingStage.AcquireSceneFrameResource(readingScene)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		data, err := lease.Bytes()
		if closeErr := lease.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return render.IndexedFrame{}, err
		}
		page, decodeErr := render.DecodeMoviePixels(data, nil)
		if len(page.Pixels) == 0 {
			return render.IndexedFrame{}, fmt.Errorf("decode book page: %w", decodeErr)
		}
		frame, err := render.StageFrame(readingStage, page.Pixels)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		border, err := loadPropFrame(propArchive, readingAction.BorderProp, "BASE", 0, readingAction.BorderDegree)
		if err != nil {
			return render.IndexedFrame{}, err
		}
		border.Origin = image.Pt(border.Origin.Y, border.Origin.X)
		return render.CompositePuppetFrame(frame, border, readingAction.BorderPoint)
	}
	closeReadingPage := func(fadeFrames int) (render.IndexedFrame, bool, error) {
		if readingStage != nil {
			if err := readingStage.Close(); err != nil {
				return render.IndexedFrame{}, false, err
			}
			readingStage = nil
		}
		if err := renderMarieInventory(); err != nil {
			return render.IndexedFrame{}, false, err
		}
		return restoreActionFrame(currentFrame, fadeFrames)
	}
	openInventoryExamination := func() (render.IndexedFrame, bool, error) {
		action, found, err := scripts.ExamineInventoryItem(workspace, handItem, inventoryDegrees[strings.ToLower(handItem)])
		if err != nil || !found {
			return currentFrame, false, err
		}
		if action.Movie != "" {
			if err := startActionMovie(action.Movie, action.FadeOutFrames, func(bool) (render.IndexedFrame, bool, error) {
				if err := renderMarieInventory(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return restoreActionFrame(currentFrame, action.FadeInFrames)
			}); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		}
		readingStage, err = workspace.OpenStage(action.Stage)
		if err != nil {
			return render.IndexedFrame{}, false, err
		}
		readingScene = -1
		for index, scene := range readingStage.Scenes {
			if strings.EqualFold(string(scene.Name[1:]), action.FlatName) {
				readingScene = index
				break
			}
		}
		if readingScene < 0 {
			_ = readingStage.Close()
			readingStage = nil
			return render.IndexedFrame{}, false, fmt.Errorf("book %s has no flat %q", action.Stage, action.FlatName)
		}
		readingAction = action
		frame, err := renderReadingPage()
		if err != nil {
			return render.IndexedFrame{}, false, err
		}
		if *debug {
			log.Printf("inventory-examine item=%s stage=%s flat=%s", handItem, action.Stage, action.FlatName)
		}
		return restoreActionFrame(frame, action.FadeInFrames)
	}
	readingMouseDown := func(point uint32) (render.IndexedFrame, bool, error) {
		pointInBorder := func(degree int16) bool {
			frame, err := loadPropFrame(propArchive, readingAction.BorderProp, "BASE", 0, degree)
			if err != nil {
				return false
			}
			frame.Origin = image.Pt(frame.Origin.Y, frame.Origin.X)
			hit, err := render.HitTestPuppetFrame(frame, readingAction.BorderPoint, image.Pt(int(int16(point>>16)), int(int16(point))))
			return err == nil && hit
		}
		readProgram := func(resource uint32, source *assets.Stage) (scripts.Program, error) {
			lease, err := source.AcquireResource(resource)
			if err != nil {
				return scripts.Program{}, err
			}
			data, err := lease.Bytes()
			if closeErr := lease.Close(); err == nil {
				err = closeErr
			}
			if err != nil {
				return scripts.Program{}, err
			}
			return scripts.ParseProgram(data)
		}
		context := scripts.InventoryBookContext{FlatIndex: readingScene + 1, FlatCount: len(readingStage.Scenes), Point: point, BorderProp: readingAction.BorderProp, BorderDegree: readingAction.BorderDegree, PointInBorder: pointInBorder}
		var action scripts.InventoryBookAction
		found := false
		if pointInBorder(readingAction.BorderDegree) {
			resource := map[string]uint32{"histbord": 196, "pagebord": 265, "yunnibord": 556}[strings.ToLower(readingAction.BorderProp)]
			data, err := propArchive.Resource(resource)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			program, err := scripts.ParseProgram(data)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			action, found, err = scripts.ParseInventoryBookAction(program, context)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
		}
		if !found || action.DelegateStage {
			program, err := readProgram(1, readingStage)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			action, found, err = scripts.ParseInventoryBookAction(program, context)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
		}
		if !found {
			return currentFrame, false, nil
		}
		if action.Close {
			return closeReadingPage(action.FadeInFrames)
		}
		previous := currentFrame
		if action.FlatName != "" {
			found := false
			for index, scene := range readingStage.Scenes {
				if strings.EqualFold(string(scene.Name[1:]), action.FlatName) {
					readingScene, found = index, true
					break
				}
			}
			if !found {
				return render.IndexedFrame{}, false, fmt.Errorf("book flat %q is unavailable", action.FlatName)
			}
		} else if action.FlatIndex > 0 && action.FlatIndex <= len(readingStage.Scenes) {
			readingScene = action.FlatIndex - 1
		}
		readingAction.BorderDegree = action.BorderDegree
		frame, err := renderReadingPage()
		if err != nil {
			return render.IndexedFrame{}, false, err
		}
		if action.Sound != "" {
			if err := soundBank.Play(audioContext, action.Sound, 1); err != nil {
				return render.IndexedFrame{}, false, err
			}
		}
		if action.VisualEffect == scripts.LookupOpcode("wipeleft") || action.VisualEffect == scripts.LookupOpcode("wiperight") {
			direction := 0
			if action.VisualEffect == scripts.LookupOpcode("wipeleft") {
				direction = 1
			}
			wipe, err := render.NewWipeEffect(previous, frame, action.Duration, direction)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode = wipe, 8
			currentFrame, stageFrame = previous, frame
		} else {
			currentFrame, stageFrame = frame, frame
		}
		if *debug {
			log.Printf("inventory-book=%s flat=%s border-degree=%d", readingAction.Item, readingStage.Scenes[readingScene].Name[1:], readingAction.BorderDegree)
		}
		return currentFrame, true, nil
	}
	returnToMainPanel := func(effect uint16, duration int) (render.IndexedFrame, bool, error) {
		oldFrame := currentFrame
		if currentScene == 2 {
			setInventoryLoopsPaused(false)
		}
		currentScene, currentPixels = 0, pixels
		inventoryMenuActive = false
		startAvatarNoFace(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
		if err := refreshWorldScene(); err != nil {
			return render.IndexedFrame{}, false, err
		}
		nextFrame := currentFrame
		if effect == scripts.LookupOpcode("barndoorclose") {
			animation, err := render.NewBarndoorClose(oldFrame, nextFrame, duration)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode, currentFrame = animation, 0, oldFrame
			if err := soundBank.Play(audioContext, "pageturn", 4); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return animation.CurrentFrame(), true, nil
		}
		return nextFrame, true, nil
	}
	// stageFlatLookup mirrors verified FUN_00413590: it resolves a flat name to
	// its zero-based index over the open stage's scenes.
	stageFlatLookup := func(stage *assets.Stage) scripts.FlatNameLookup {
		return func(name string) (int, error) {
			for index := range stage.Scenes {
				if strings.EqualFold(string(stage.Scenes[index].Name[1:]), name) {
					return index, nil
				}
			}
			return -1, scripts.ErrUnknownFlat
		}
	}
	// currentFlatName is the Go equivalent of the native currentflat() builtin:
	// it yields the name of the flat currently on screen, which is what a
	// gotoflat string argument is resolved against.
	currentFlatName := func(stage *assets.Stage, scene int) string {
		if scene < 0 || scene >= len(stage.Scenes) {
			return ""
		}
		return string(stage.Scenes[scene].Name[1:])
	}
	// applyFlatTarget performs one verified gotoflat transition. It is shared by
	// the score-menu button scripts and by the dynamic gotoflat(<identifier>)
	// return that resource 24 executes after savegame, so both take the same
	// scene-load, palette, inventory and transition path.
	applyFlatTarget = func(target int, effect uint16, duration int, source uint32) (render.IndexedFrame, bool, error) {
		mainFlat := stage == mainStage
		if target < 0 || target >= len(stage.Scenes) {
			return render.IndexedFrame{}, false, fmt.Errorf("flat script resource %d selects stage scene %d", source, target)
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
		if mainFlat && target == 3 {
			scoreVolumeBaseFrame = nextFrame
			slider, status, err := scripts.NativeMenuVolume(audio.WaveVolume(), func(int) error { return nil })
			if err != nil || status != 0 {
				return render.IndexedFrame{}, false, fmt.Errorf("read score volume slider: status=%#x err=%v", status, err)
			}
			nextFrame, scoreVolumeTrack, err = composeScoreVolume(scoreVolumeBaseFrame, slider)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
		} else {
			volumeSliderDragging = false
			scoreVolumeTrack = image.Rectangle{}
		}
		if mainFlat && target == 0 {
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
		previousScene := currentScene
		if mainFlat && previousScene == 2 && target != 2 {
			setInventoryLoopsPaused(false)
		}
		if mainFlat && previousScene != 2 && target == 2 {
			setInventoryLoopsPaused(true)
		}
		currentScene, currentPixels = target, nextPixels
		inventoryMenuActive = mainFlat && target == 2
		if inventoryMenuActive {
			previousFrame := currentFrame
			if err := renderMarieInventory(); err != nil {
				return render.IndexedFrame{}, false, err
			}
			nextFrame, currentFrame = currentFrame, previousFrame
		} else if mainFlat && target == 0 {
			stageFrame = nextFrame
			if previousScene == 2 {
				startAvatarNoFace(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
			}
		} else if !mainFlat {
			stageFrame = nextFrame
		}
		if isaoInteractionStage == isaoInteractionInventory && target == 0 {
			isaoInventoryReturnPending, isaoInteractionStage = true, isaoInteractionInventoryReturning
		}
		if marieInteractionStage == marieInteractionInventory && target == 0 {
			marieInventoryReturnPending, marieInteractionStage = true, marieInteractionInventoryReturning
		}
		if trotterInteractionStage == trotterInteractionInventory && target == 0 {
			trotterInventoryReturnPending, trotterInteractionStage = true, trotterInteractionInventoryReturning
		}
		if laurelInteractionStage == laurelInteractionInventory && target == 0 {
			laurelInventoryReturnPending, laurelInteractionStage = true, laurelInteractionInventoryReturning
		}
		if effect != 0 {
			if _, err := runNativeScheduler(true); err != nil {
				return render.IndexedFrame{}, false, err
			}
			effectName := ""
			switch effect {
			case scripts.LookupOpcode("barndooropen"):
				effectName = "barndooropen"
				transition, err = render.NewBarndoorOpen(currentFrame, nextFrame, duration)
			case scripts.LookupOpcode("barndoorclose"):
				effectName = "barndoorclose"
				transition, err = render.NewBarndoorClose(currentFrame, nextFrame, duration)
			case scripts.LookupOpcode("plain"):
				effectName = "plain"
				if duration > 0 {
					transition, err = render.NewFadeEffect(currentFrame, nextFrame, duration)
				} else {
					currentFrame = nextFrame
				}
			default:
				return render.IndexedFrame{}, false, fmt.Errorf("flat script resource %d uses unsupported visual effect %d", source, effect)
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
				log.Printf("visualeffect=%s duration=%d", effectName, duration)
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
	}
	initialFrame := playback.CurrentFrame()
	if *loadPath != "" {
		if err := releaseMovieAudio(); err != nil {
			return err
		}
		if err := movie.Close(); err != nil {
			return err
		}
		movie, playback = nil, nil
		if err := loadGameProgressFile(*loadPath); err != nil {
			return err
		}
		initialFrame = currentFrame
	}
	if *silent && *silentScript == "" {
		if err := render.WritePNG(filepath.Join(workspace.WorkDir, "redust-silent.png"), initialFrame); err != nil {
			return err
		}
		return stage.Close()
	}
	runGame := engine.Run
	if *silentScript != "" {
		runGame = engine.SilentRunner(*silentScript, func() map[string]any {
			movieFrame, movieWaiting := -1, false
			if playback != nil {
				movieFrame, movieWaiting = playback.FrameIndex(), playback.WaitingForInput()
			}
			return map[string]any{"day": gameDay, "clock": gameClock, "phase": gamePhase, "set": activeSetName, "scene": string(stage.Scenes[currentScene].Name[1:]), "point": worldPoint, "cash": playercash, "inventory": inventoryOwners, "inventoryMenu": inventoryMenuActive, "breakfast": hotelHotplateStage != nil && stage == hotelHotplateStage, "jonesPhase": jonesPhase, "jonesRingStory": jonesRingStory, "laurelPhase": laurelPhase, "movieFrame": movieFrame, "movieWaiting": movieWaiting, "scriptActors": scriptActors.Snapshot([]string{"mwife", "blood", "buick"})}
		})
	}
	runErr := runGame(initialFrame, func() (render.IndexedFrame, bool, error) {
		displayChanged := false
		drainMovieAudioTails()
		if scriptTask != nil {
			if convDialogue != nil && convDialogue.Active() {
				frame, changed, err := convDialogue.Update(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
				if err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("advance script conversation: %w", err)
				}
				if changed {
					currentFrame, stageFrame, displayChanged = frame, frame, true
				}
			}
			if scriptHost.PassRequested() && nativePumpDue() {
				if _, err := runNativeScheduler(false); err != nil {
					return render.IndexedFrame{}, false, err
				}
				scriptHost.Passed()
				if err := refreshWorldScene(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				displayChanged = true
			}
			done, err := scriptTask.Poll()
			if done {
				label := scriptTask.Label
				scriptTask = nil
				scriptHost.SetTask(nil)
				if err != nil {
					log.Printf("script-task %s: %v", label, err)
				} else if *debug {
					log.Printf("script-task %s complete", label)
				}
				displayChanged = true
			}
			return currentFrame, displayChanged, nil
		}
		if hotelVoice != nil {
			done, err := hotelVoice.Update(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if done {
				if err := hotelVoice.Close(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				hotelVoice = nil
				if err := startHotelVoiceStep(); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
		}
		if hotelDelayUntil != 0 && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= hotelDelayUntil {
			hotelDelayUntil = 0
			if hotelDoorAfterDelay != "" {
				doorOwner, hotelDoorAfterDelay = hotelDoorAfterDelay, ""
			} else if len(hotelVoiceSteps) > 0 {
				if err := startHotelVoiceStep(); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
		}
		if deathStage == 1 && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= deathUntil {
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, deathSequence.FadeOutFrames)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode, deathStage = fade, 5, 2
			return fade.CurrentFrame(), true, nil
		}
		if deathStage == 5 {
			finished := deathNarration != nil && !deathNarration.IsPlaying() || *silent && scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) >= deathUntil
			if finished {
				if deathNarration != nil {
					if err := deathNarration.Close(); err != nil {
						return render.IndexedFrame{}, false, err
					}
					deathNarration = nil
				}
				if err := deathBank.Close(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				deathBank, deathStage, handItem = nil, 0, ""
				if *debug {
					log.Printf("death-flat=ready cause=%s", deathSequence.Cause)
				}
			}
			return currentFrame, finished, nil
		}
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
		if playback == nil && transition == nil && pendingSpotMovie != "" {
			spotMovieName, pendingSpotMovie = pendingSpotMovie, ""
			spotMovieReturnFrame, spotMovieActionFrameOne, spotMovieActive = currentFrame, false, true
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, 10)
			if err != nil {
				spotMovieName, spotMovieActive = "", false
				return render.IndexedFrame{}, false, fmt.Errorf("premovie screentoblack: %w", err)
			}
			transition, transitionMode = fade, 4
			if *debug {
				log.Printf("spotmovie=%s premovie=screentoblack duration=10", spotMovieName)
			}
			return fade.CurrentFrame(), true, nil
		}
		if playback == nil && transition == nil && pendingSceneMovie != "" {
			name := pendingSceneMovie
			pendingSceneMovie = ""
			if err := startSceneMovie(name); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return playback.CurrentFrame(), true, nil
		}
		if playback == nil && transition == nil {
			if readingStage != nil {
				return currentFrame, false, nil
			}
			avatarChanged, err := advanceAvatarAnimation(scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()))
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("advance player portrait: %w", err)
			}
			displayChanged = avatarChanged
			changed := false
			if nativePumpDue() {
				var err error
				if changed, err = runNativeScheduler(false); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
			if currentScene == 2 && inventoryMenuActive && inventoryCashNeedsRefresh(inventoryCashRendered, inventoryCashRenderedValid, playercash) {
				if err := renderMarieInventory(); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("refresh inventory cash: %w", err)
				}
				return currentFrame, true, nil
			}
			displayChanged = displayChanged || changed
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
		if playback == nil && transition == nil && currentScene == 0 && (pendingMovement != 0 || pendingPlayerMovement != 0) {
			movement := pendingMovement
			if pendingMovement != 0 {
				pendingMovement = 0
			} else {
				movement, pendingPlayerMovement = pendingPlayerMovement, 0
			}
			nextPoint, transitionResource, found, err := activeSet.MovePoint(worldPoint, movement)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("move in %s: %w", activeSetName, err)
			}
			if !found {
				if *debug {
					log.Printf("level-move blocked movement=%d point=%v", movement, worldPoint)
				}
				return currentFrame, displayChanged, nil
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
				log.Printf("level-move=%d point=%v transition-resource=%d frame-resource=%d set=%s view=%s day=%d clock=%d", movement, worldPoint, transitionResource, frameResource, activeSetName, view.Name[1:], gameDay, gameClock)
				for _, actor := range projectedActors {
					log.Printf("world-actor=%s depth=%d bounds=%d,%d,%d,%d", actor.Name, actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
				}
			}
			return nextFrame, true, nil
		}
		if playback == nil {
			if transition == nil {
				return currentFrame, displayChanged, nil
			}
			transitionFrame, changed, done := transition.Update()
			if done {
				if transitionMode == 7 {
					currentFrame, transition, transitionMode = transition.TargetFrame(), nil, 0
					if err := startSceneMovie(actionMovieName); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return playback.CurrentFrame(), true, nil
				}
				if transitionMode == 8 {
					currentFrame, stageFrame, transition, transitionMode = transition.TargetFrame(), transition.TargetFrame(), nil, 0
					return currentFrame, true, nil
				}
				if transitionMode == 9 {
					currentFrame, transition, transitionMode = transition.TargetFrame(), nil, 0
					action := hotelHotplatePending
					hotelHotplatePending = scripts.HotelAction{}
					frame, err := openHotelHotplate(action)
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					if action.VisualEffect == "irisopen" {
						iris, err := render.NewIrisOpenEffect(blackFrame, frame, action.FadeInFrames)
						if err != nil {
							return render.IndexedFrame{}, false, err
						}
						transition, transitionMode, currentFrame = iris, 8, iris.CurrentFrame()
						return currentFrame, true, nil
					}
					return frame, true, nil
				}
				if transitionMode == 10 {
					currentFrame, transition, transitionMode = transition.TargetFrame(), nil, 0
					action := hotelHotplatePending
					hotelHotplatePending = scripts.HotelAction{}
					if err := hotelHotplateStage.Close(); err != nil {
						return render.IndexedFrame{}, false, err
					}
					if hotelHotplateBank != nil {
						if err := hotelHotplateBank.Close(); err != nil {
							return render.IndexedFrame{}, false, err
						}
					}
					hotelHotplateStage, hotelHotplateBank, stage = nil, nil, mainStage
					if _, _, err := applyFlatTarget(0, 0, 0, 0); err != nil {
						return render.IndexedFrame{}, false, err
					}
					if err := refreshWorldScene(); err != nil {
						return render.IndexedFrame{}, false, err
					}
					if *debug {
						log.Printf("hotel-stage=closed track=%s return=%s set=%s point=%v phase=%d", action.CloseTrack, action.ReturnTarget, activeSetName, worldPoint, gamePhase)
					}
					return restoreActionFrame(currentFrame, action.FadeInFrames)
				}
				if transitionMode == 5 {
					transition, transitionMode = nil, 0
					if err := startDeathMovie(); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return playback.CurrentFrame(), true, nil
				}
				if transitionMode == 6 {
					currentFrame, stageFrame, transition, transitionMode = deathFrame, deathFrame, nil, 0
					if err := startDeathNarration(); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				if transitionMode == 4 {
					currentFrame, stageFrame, transition, transitionMode = transition.TargetFrame(), transition.TargetFrame(), nil, 0
					spotMovieStarting = true
					err := startSceneMovie(spotMovieName)
					spotMovieStarting = false
					if err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("start spotmovie %s: %w", spotMovieName, err)
					}
					return playback.CurrentFrame(), true, nil
				}
				if transitionMode == 3 {
					completedSpotMovie := spotMovieName
					currentFrame, stageFrame, transition, transitionMode = transition.TargetFrame(), transition.TargetFrame(), nil, 0
					spotMovieActive, spotMovieName = false, ""
					if isaoKeyMovieActive {
						isaoKeyMovieActive = false
						action, found := scripts.IsaoKeyMovieResponse(spotMovieActionFrameOne, inventoryOwners["ring"])
						if !found {
							if err := returnIsaoToIdle(); err != nil {
								return render.IndexedFrame{}, false, err
							}
							return currentFrame, true, nil
						}
						if action.AddInventoryItem != "" {
							inventoryOwners[action.AddInventoryItem], handItem = "stranger", action.AddInventoryItem
							if err := soundBank.Play(audioContext, "inven", 1); err != nil {
								return render.IndexedFrame{}, false, fmt.Errorf("play Isao ring inventory sound: %w", err)
							}
							if err := returnIsaoToIdle(); err != nil {
								return render.IndexedFrame{}, false, err
							}
							return currentFrame, true, nil
						}
						if action.RunPuppet != "" {
							if action.SetIsaoPhaseValid {
								isaoPhase = action.SetIsaoPhase
							}
							isaoSecondRun = true
							isaoDelayUntil = scripts.NativeFrameUnits(scripts.NativeTickMilliseconds()) + uint32(action.DelayFrames)
							isaoInteractionStage = isaoInteractionPuppetDelay
							if *debug {
								log.Printf("actor=isao movie=%s actionframe=%t ring-owner=%s followup=%s delay=%d", completedSpotMovie, spotMovieActionFrameOne, inventoryOwners["ring"], action.RunPuppet, action.DelayFrames)
							}
							return currentFrame, true, nil
						}
					}
					if *debug {
						log.Printf("spotmovie=%s postmovie=restored clock=%d", completedSpotMovie, gameClock)
					}
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
					}
					return currentFrame, true, nil
				}
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
		if warning := playback.TakeDecodeWarning(); warning != nil {
			movieWarningCount[movieIndex]++
			if movieWarningSample[movieIndex] == "" {
				movieWarningSample[movieIndex] = warning.Error()
			}
		}
		if !done {
			return movieFrame, changed, nil
		}
		if spotMovieActive || sceneMovieAfter != nil {
			spotMovieActionFrameOne, err = playback.ActionFrame(1)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("read spotmovie actionframe(1): %w", err)
			}
		}
		if *debug && movieWarningCount[movieIndex] > 0 {
			log.Printf("movie=%s decode-warnings=%d first=%s", movieNames[movieIndex], movieWarningCount[movieIndex], movieWarningSample[movieIndex])
		}
		if err := releaseMovieAudio(); err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("stop startup soundtrack %s: %w", movieNames[movieIndex], err)
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
			movieAudio, movieAudioEvents, movieAudioLoop, err = startNativeMovieAudio(audioContext, movie)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			movieAudioEmbedded = nativeMovieHasEmbeddedAudio(movie)
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
		if sceneMovieAfter != nil {
			after := sceneMovieAfter
			sceneMovieAfter = nil
			return after(spotMovieActionFrameOne)
		}
		if deathStage == 3 {
			fade, err := render.NewFadeEffect(blackFrame, deathFrame, deathSequence.FadeInFrames)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode, deathStage = fade, 6, 4
			return fade.CurrentFrame(), true, nil
		}
		if spotMovieActive {
			blackScreen := currentFrame
			returnSet := nativeSpotMovieReturnSet(gameClock)
			if activeSetName == "town" && currentScene == 0 {
				direction := "north"
				switch worldPoint[2] {
				case assets.SetDirectionEast:
					direction = "east"
				case assets.SetDirectionSouth:
					direction = "south"
				case assets.SetDirectionWest:
					direction = "west"
				}
				returnFrame, err := switchSpecialSet(returnSet, string(view.Name[1:]), direction)
				if err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("restore town after spotmovie: %w", err)
				}
				spotMovieReturnFrame = returnFrame
			}
			currentFrame, stageFrame = blackScreen, blackScreen
			fade, err := render.NewFadeEffect(blackScreen, spotMovieReturnFrame, 30)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("postmovie fade to saved scene: %w", err)
			}
			transition, transitionMode = fade, 3
			if *debug {
				log.Printf("spotmovie=%s postmovie=blacktoscreen clock=%d actionframe1=%t duration=30", spotMovieName, gameClock, spotMovieActionFrameOne)
			}
			return fade.CurrentFrame(), true, nil
		}
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
		if themePlayer == nil && currentScene == 0 && activeSetName == "town" {
			themeBank, err = audio.OpenSoundBank(workspace, "DATA/NIGHT.SND")
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("open startup theme bank: %w", err)
			}
			theme, err := themeBank.LoadTheme("town.snd")
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("load startup town theme: %w", err)
			}
			currentTheme = theme
			themePlayer, err = theme.Play(audioContext)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("start startup town theme: %w", err)
			}
			currentThemeName = theme.FirstVoiceName()
			resetNativeRandom()
			if gameClock == 3 {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "leroy", Callback: "leroyidle", Remaining: 20}); status != 0 {
					return render.IndexedFrame{}, false, fmt.Errorf("register Leroy idle loop returned status %#x", status)
				}
			}
			if gameDay == 1 {
				step, found := scripts.DogIdleStep("doright", &nativeRandom)
				if found {
					actorPoses["dog"] = step.Pose
					if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "dog", Callback: step.Callback, Remaining: step.Remaining}); status != 0 {
						return render.IndexedFrame{}, false, fmt.Errorf("register dog idle loop returned status %#x", status)
					}
				}
			}
			if marieVisible {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindActor, Owner: "marie", Callback: "marieidle", Remaining: 17}); status != 0 {
					return render.IndexedFrame{}, false, fmt.Errorf("register Marie idle loop returned status %#x", status)
				}
			}
			if currentThemeName == "nightwind3" {
				if status := nativeLoops.Register(scripts.ScriptLoop{Kind: scripts.LoopKindScene, Owner: "scene g14", Callback: "nightfxs", Remaining: 2}); status != 0 {
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
		if hotelEventsLocked {
			return
		}
		if readingStage != nil {
			if key == ebiten.KeyEscape {
				if _, _, err := closeReadingPage(30); err != nil {
					log.Printf("close inventory book: %v", err)
				}
			}
			return
		}
		if deathStage != 0 && playback == nil {
			return
		}
		waveVolume := -1
		switch key {
		case ebiten.Key0:
			waveVolume = 0
		case ebiten.Key1:
			waveVolume = 1
		case ebiten.Key2:
			waveVolume = 2
		case ebiten.Key3:
			waveVolume = 3
		case ebiten.Key4:
			waveVolume = 4
		case ebiten.Key5:
			waveVolume = 5
		case ebiten.Key6:
			waveVolume = 6
		case ebiten.Key7:
			waveVolume = 7
		case ebiten.Key8:
			waveVolume = 8
		case ebiten.Key9:
			waveVolume = 9
		}
		if waveVolume >= 0 {
			slider, _, err := setScoreMenuVolume(waveVolume)
			if err != nil {
				log.Printf("set wave volume: %v", err)
			} else if *debug {
				log.Printf("wavevolume=%d slider=%d,%d", waveVolume, slider.X, slider.Y)
			}
			return
		}
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
		if laurelInteractionStage == laurelInteractionSpeaking {
			if key == ebiten.KeySpace || key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				laurelDialogueSkip = true
			}
			return
		}
		if laurelInteractionStage == laurelInteractionChoices {
			switch key {
			case ebiten.KeyArrowUp, ebiten.KeyW:
				if len(laurelChoices) > 0 {
					if laurelChoiceOutline <= 0 {
						laurelChoiceOutline = len(laurelChoices) - 1
					} else {
						laurelChoiceOutline--
					}
					if err := drawLaurelChoices(laurelChoiceOutline); err != nil {
						log.Printf("draw Laurel choice: %v", err)
					}
				}
			case ebiten.KeyArrowDown, ebiten.KeyS:
				if len(laurelChoices) > 0 {
					laurelChoiceOutline = (laurelChoiceOutline + 1) % len(laurelChoices)
					if err := drawLaurelChoices(laurelChoiceOutline); err != nil {
						log.Printf("draw Laurel choice: %v", err)
					}
				}
			case ebiten.KeyEnter, ebiten.KeySpace:
				if laurelChoiceOutline >= 0 && laurelChoiceOutline < len(laurelChoices) {
					if err := startLaurelChoice(laurelChoices[laurelChoiceOutline].EventID); err != nil {
						log.Printf("run Laurel choice: %v", err)
					}
				}
			}
			return
		}
		if laurelInteractionStage != laurelInteractionIdle && laurelInteractionStage != laurelInteractionInventory {
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
		if trotterInteractionStage == trotterInteractionPuppetSpeaking {
			if key == ebiten.KeySpace || key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				trotterDialogueSkip = true
			}
			return
		}
		if trotterInteractionStage == trotterInteractionPuppetChoices {
			if key == ebiten.KeyEscape || key == ebiten.KeyQ || key == ebiten.KeyPeriod {
				if err := startTrotterEventResponse(-1); err != nil {
					log.Printf("cancel Trotter choice: %v", err)
				}
			}
			return
		}
		if trotterInteractionStage != trotterInteractionIdle {
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
			if dog2OfferStage != dog2OfferIdle {
				return
			}
			if currentScene == 0 {
				switch key {
				case ebiten.KeyArrowUp, ebiten.KeyW:
					if action, found := scripts.HotelForwardAction(activeSetName, view.Resource, worldPoint[2], doorOwner, hotelStoryState()); found {
						if err := queueHotelTransition(action); err != nil {
							log.Printf("hotel transition: %v", err)
						}
						return
					}
					if activeSetName == "town" {
						if target, targetScene, targetDirection, found := scripts.NiteInteriorTargetForState(view.Resource, worldPoint[2], doorOwner, gameClock); found {
							pendingSetName, pendingSetScene, pendingSetDirection, doorOwner, pendingPlayerMovement = target, targetScene, targetDirection, "", 0
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
					if name, blocked := scripts.NiteDogGateMovieInSet(activeSetName, view.Resource, worldPoint[2], gameDay, dogVisible); blocked {
						pendingMovement, pendingPlayerMovement = 0, 0
						pendingSpotMovie = name
						dogMovieNeedsHelp = true
						if *debug {
							log.Printf("event=NITE.SET/key-down dog-gate point=%v movie=%s", worldPoint, name)
						}
						return
					}
					pendingPlayerMovement = assets.SceneMoveStraight
				case ebiten.KeyArrowDown, ebiten.KeyS:
					pendingPlayerMovement = assets.SceneMoveBackwards
				case ebiten.KeyArrowLeft, ebiten.KeyA:
					pendingPlayerMovement = assets.SceneMoveLeft
				case ebiten.KeyArrowRight, ebiten.KeyD:
					pendingPlayerMovement = assets.SceneMoveRight
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
		if scriptTask != nil {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			if convDialogue != nil && convDialogue.Active() {
				frame, _, err := convDialogue.Skip()
				if err != nil {
					return render.IndexedFrame{}, false, err
				}
				convSkipped, currentFrame, stageFrame = true, frame, frame
				return currentFrame, true, nil
			}
			if convChoosing {
				position := image.Pt(int(int16(mouseEvent.Point>>16)), int(int16(mouseEvent.Point)))
				for index := range convChoices {
					if position.In(image.Rect(0, 264+index*24, 512, 288+index*24)) {
						convPressIndex = index
						if err := drawConvChoices(index); err != nil {
							return render.IndexedFrame{}, false, err
						}
						return currentFrame, true, nil
					}
				}
			}
			return currentFrame, false, nil
		}
		point := mouseEvent.Point
		if hotelEventsLocked {
			return currentFrame, false, nil
		}
		if deathStage != 0 {
			return currentFrame, false, nil
		}
		if playback != nil {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			action, hit, err := playback.Click(image.Pt(int(int16(point>>16)), int(int16(point))))
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if !hit {
				return currentFrame, false, nil
			}
			if action.Kind == render.MovieInputUnhandled {
				return render.IndexedFrame{}, false, fmt.Errorf("movie input type %d is not implemented", action.Event.Type)
			}
			if action.NextResourceOffset != 0 {
				return render.IndexedFrame{}, false, fmt.Errorf("movie input chains to unimplemented resource offset %d", action.NextResourceOffset)
			}
			if err := playMovieInputSound(action.Event.Argument); err != nil {
				return render.IndexedFrame{}, false, err
			}
			currentFrame = playback.CurrentFrame()
			if *debug {
				log.Printf("movie-input kind=%d type=%d argument=%d frame=%d next-resource=%d", action.Kind, action.Event.Type, action.Event.Argument, action.Frame, action.NextResourceOffset)
			}
			return currentFrame, true, nil
		}
		if transition != nil {
			return render.IndexedFrame{}, false, nil
		}
		if readingStage != nil {
			if mouseEvent.Button == ebiten.MouseButtonLeft {
				return readingMouseDown(point)
			}
			return currentFrame, false, nil
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
		if laurelInteractionStage == laurelInteractionSpeaking {
			return currentFrame, false, nil
		}
		if laurelInteractionStage == laurelInteractionChoices {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			event, found := scripts.NativePuppetChoiceAt(point, laurelChoices)
			if !found {
				return currentFrame, false, nil
			}
			laurelChoicePressActive, laurelChoicePressEvent = true, event
			laurelChoicePressIndex = (int(int16(point)) - 264) / 24
			return currentFrame, false, nil
		}
		if laurelInteractionStage != laurelInteractionIdle && laurelInteractionStage != laurelInteractionInventory {
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
		if trotterInteractionStage == trotterInteractionPuppetChoices {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			event, found := scripts.NativePuppetChoiceAt(point, trotterActiveChoices)
			if !found {
				return currentFrame, false, nil
			}
			trotterChoicePressActive, trotterChoicePressEvent = true, event
			trotterChoicePressIndex = (int(int16(point)) - 264) / 24
			return currentFrame, false, nil
		}
		if inventoryMenuActive && currentScene == 2 && mouseEvent.Button == ebiten.MouseButtonLeft {
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
		if trotterInteractionStage != trotterInteractionIdle && trotterInteractionStage != trotterInteractionInventory {
			return currentFrame, false, nil
		}
		if isaoInteractionStage != isaoInteractionIdle && isaoInteractionStage != isaoInteractionInventory {
			return currentFrame, false, nil
		}
		if jonesInteractionStage != jonesInteractionIdle {
			return currentFrame, false, nil
		}
		if stage == mainStage && currentScene == 3 && mouseEvent.Button == ebiten.MouseButtonLeft {
			if level, found := scoreMenuVolumeAt(point, scoreVolumeTrack); found {
				volumeSliderDragging = true
				if _, _, err := setScoreMenuVolume(level); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("set score menu volume: %w", err)
				}
				return currentFrame, true, nil
			}
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
			log.Printf("mouse scene=%s point=%d,%d hit=%t handler=%s set=%s view=%s world-point=%v day=%d clock=%d", stage.Scenes[currentScene].Name[1:], int16(point>>16), int16(point), hit, handlerName, activeSetName, view.Name[1:], worldPoint, gameDay, gameClock)
			for _, actor := range projectedActors {
				log.Printf("view-actor=%s depth=%d bounds=%d,%d,%d,%d", actor.Name, actor.Depth, actor.Bounds.Min.X, actor.Bounds.Min.Y, actor.Bounds.Max.X, actor.Bounds.Max.Y)
			}
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
			if activeSetName == "town" {
				if action, found := scripts.NiteSceneObjectAction(view.Resource, worldPoint[2], point, gameDay, gameClock); found {
					if *debug {
						log.Printf("world-object=%s movie=%s", action.Object, action.Movie)
					}
					if err := startSceneMovie(action.Movie); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
			}
			if mouseEvent.Button == ebiten.MouseButtonLeft {
				if action, found := scripts.HotelMouseAction(activeSetName, view.Resource, worldPoint[2], point, hotelStoryState()); found {
					return runHotelAction(action)
				}
			}
			if mouseEvent.Button == ebiten.MouseButtonLeft && activeSetName == "town" {
				if owner, found := scripts.NiteDoorAt(view.Resource, worldPoint[2], point); found {
					locked, err := scripts.NiteDoorLocked(owner, gameDay, gameClock, gamePhase, false, false, &nativeRandom, inventoryOwners)
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					if owner == "back" && inventoryOwners["hhkey"] != "stranger" {
						locked = true
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
				if scriptManaged[strings.ToLower(actorName)] {
					if err := startScriptTask(scripts.ActorEvent{Actor: actorName, Message: "mousedown(0)"}); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				if strings.EqualFold(actorName, "Trotter") {
					started, err := beginTrotterPuppetTalk()
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, started, nil
				}
				if strings.EqualFold(actorName, "Laurel") {
					started, err := beginLaurelPuppetTalk()
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, started, nil
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
						nativeLoops.Stop(scripts.LoopKindActor, "leroy")
						if *debug {
							log.Printf("actor=leroy walktopuppet distance=%d destination=%v heading=%d", distance, destination, routeHeading)
						}
						return currentFrame, true, nil
					}
				}
				if strings.EqualFold(actorName, "Jones") && jonesInteractionStage == jonesInteractionIdle {
					if err := startAvatarTiphat(func() (bool, error) { return beginJonesPuppetTalk() }); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				if strings.EqualFold(actorName, "Marie") && marieInteractionStage == marieInteractionIdle {
					if err := startAvatarTiphat(func() (bool, error) { return beginMariePuppetTalk() }); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
			}
		}
		if !hit {
			if hotelHotplateStage != nil && stage == hotelHotplateStage && mouseEvent.Button == ebiten.MouseButtonLeft {
				if action, found := scripts.HotelHotplateBackgroundAction(string(stage.Scenes[currentScene].Name[1:])); found {
					return runHotelAction(action)
				}
			}
			return render.IndexedFrame{}, false, nil
		}
		if hotelHotplateStage != nil && stage == hotelHotplateStage && mouseEvent.Button == ebiten.MouseButtonLeft {
			if action, found := scripts.HotelHotplateMouseAction(handler.ScriptResource, hotelStoryState()); found {
				return runHotelAction(action)
			}
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
		if stage == mainStage && currentScene == 4 {
			action, found, err := scripts.ParseDeathButtonAction(program, string(handler.Name[1:]), *debug)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if found && action.Kind == scripts.DeathButtonNew {
				if err := startNewGame(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if found && action.Kind == scripts.DeathButtonHelp {
				if err := startSceneMovie(action.Movie); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return playback.CurrentFrame(), true, nil
			}
			if found && action.Kind == scripts.DeathButtonQuit && *debug {
				return currentFrame, false, ebiten.Termination
			}
		}
		continuation, trackButton, err := scripts.ParseFlatMouseContinuation(program, string(handler.Name[1:]))
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("parse flat button script resource %d: %w", handler.ScriptResource, err)
		}
		if trackButton {
			flatMouseResource = handler.ScriptResource
			flatMouseReturnFlat = currentFlatName(stage, currentScene)
			flatMouseSession = new(scripts.FlatMouseSession)
			*flatMouseSession = scripts.NewFlatMouseSession(string(handler.Name[1:]), point, continuation)
			return currentFrame, false, nil
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
		return applyFlatTarget(action.FlatTarget, action.VisualEffect, action.Duration, handler.ScriptResource)
	}, func(state engine.MouseState) (render.IndexedFrame, bool, error) {
		if scriptTask != nil {
			if !convChoosing || convPressIndex < 0 || !state.LeftReleased {
				return currentFrame, false, nil
			}
			// The native bevel commits on release over the pressed row.
			pressed := convPressIndex
			convPressIndex = -1
			position := image.Pt(int(int16(state.Point>>16)), int(int16(state.Point)))
			if position.In(image.Rect(0, 264+pressed*24, 512, 288+pressed*24)) {
				convChosen, convHasChoice, convChoosing = convChoices[pressed].EventID, true, false
				return currentFrame, false, nil
			}
			if err := drawConvChoices(-1); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		}
		updateNativeCursor(state.Point)
		if volumeSliderDragging {
			changed := false
			if level, found := scoreMenuVolumeAt(state.Point, scoreVolumeTrack); found && (state.LeftDown || state.LeftReleased) {
				if _, _, err := setScoreMenuVolume(level); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("track score menu volume: %w", err)
				}
				changed = true
			}
			if state.LeftReleased || !state.LeftDown {
				volumeSliderDragging = false
			}
			return currentFrame, changed, nil
		}
		if flatMouseSession != nil {
			handler, hit, err := stage.HitTestSceneHandler(currentScene, state.Point)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("track flat button: %w", err)
			}
			inside := hit && handler.ScriptResource == flatMouseResource
			status := flatMouseSession.Advance(state.Point, state.LeftDown, state.LeftReleased, inside)
			if status == scripts.FlatMousePending {
				return currentFrame, false, nil
			}
			continuation, resumed := flatMouseSession.Resume()
			returnFlatName := flatMouseReturnFlat
			resourceSource := flatMouseResource
			flatMouseSession, flatMouseResource, flatMouseReturnFlat = nil, 0, ""
			if status == scripts.FlatMouseCancel || !resumed {
				return currentFrame, false, nil
			}
			switch continuation.Action {
			case scripts.FlatMouseActionExamineInventory:
				return openInventoryExamination()
			case scripts.FlatMouseActionSaveGame:
				if err := saveGameProgress(continuation.GameName); err != nil {
					log.Printf("save game: %v", err)
					return currentFrame, false, nil
				}
				// Resource 24 continues with gotoflat(<identifier>), which restores
				// the flat captured by currentflat() before its gotoflat(1). Go does
				// not yet perform that intermediate transition or the native file
				// dialog, so this return normally rebuilds the flat the player is
				// already on, matching the handler's trailing flat "update" loop.
				// Resource 24 continues with gotoflat(<identifier>). Verified
				// FUN_00411AD0: a non-numeric argument is resolved by a flat name
				// lookup and incremented, so returning to the flat captured by
				// `arg = currentflat()` routes back to the same flat. Go does not
				// yet perform the intermediate gotoflat(1) or the native file
				// dialog, so this normally rebuilds the flat already on screen,
				// which is what the handler's trailing flat "update" loop achieves.
				if continuation.ReturnToCurrentFlat && returnFlatName != "" {
					resolved, status, resolveErr := scripts.ResolveGotoflatTarget(true, scripts.GotoflatValue{
						Type: 3,
						Name: returnFlatName,
					}, stageFlatLookup(stage))
					if resolveErr != nil || status != 0 {
						if *debug {
							log.Printf("save=written name=%q gotoflat-return unresolved flat=%q status=%#x err=%v", continuation.GameName, returnFlatName, status, resolveErr)
						}
						return currentFrame, false, nil
					}
					if *debug {
						log.Printf("save=written name=%q gotoflat-return flat=%q native-index=%d return-ident=%q", continuation.GameName, returnFlatName, resolved, continuation.ReturnIdentifier)
					}
					return applyFlatTarget(resolved-1, 0, 0, resourceSource)
				}
				if *debug {
					log.Printf("save=written name=%q no-return-flat", continuation.GameName)
				}
				return currentFrame, false, nil
			case scripts.FlatMouseActionOpenGame:
				if err := loadGameProgress(continuation.GameName); err != nil {
					log.Printf("open game: %v", err)
					return currentFrame, false, nil
				}
				// Resource 25 restarts the flat "update" loop only when
				// currentflat() == "score"; the load itself already rebuilds the
				// scene, so no second transition is issued.
				if *debug {
					log.Printf("open=loaded name=%q return-to-current-flat=%t", continuation.GameName, continuation.ReturnToCurrentFlat)
				}
				return currentFrame, true, nil
			case scripts.FlatMouseActionGoToFlat:
				if continuation.FlatTarget == 0 {
					return returnToMainPanel(continuation.VisualEffect, continuation.Duration)
				}
			}
			return currentFrame, false, nil
		}
		if laurelInteractionStage == laurelInteractionChoices && laurelChoicePressActive {
			event, found := scripts.NativePuppetChoiceAt(state.Point, laurelChoices)
			outline := -1
			if found && event == laurelChoicePressEvent {
				outline = (int(int16(state.Point)) - 264) / 24
			}
			if state.LeftDown {
				if outline == laurelChoiceOutline {
					return currentFrame, false, nil
				}
				if err := drawLaurelChoices(outline); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				selected, selectedEvent := outline >= 0 && outline == laurelChoicePressIndex, laurelChoicePressEvent
				laurelChoicePressActive, laurelChoicePressIndex = false, -1
				if selected {
					if err := startLaurelChoice(selectedEvent); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("run Laurel choice %d: %w", selectedEvent, err)
					}
					return currentFrame, true, nil
				}
				if laurelChoiceOutline >= 0 {
					if err := drawLaurelChoices(-1); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				return currentFrame, false, nil
			}
			return currentFrame, false, nil
		}
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
		if trotterInteractionStage == trotterInteractionPuppetChoices && trotterChoicePressActive {
			event, found := scripts.NativePuppetChoiceAt(state.Point, trotterActiveChoices)
			outline := -1
			if found && event == trotterChoicePressEvent {
				outline = (int(int16(state.Point)) - 264) / 24
			}
			if state.LeftDown {
				if outline == trotterChoiceOutline {
					return currentFrame, false, nil
				}
				if err := drawTrotterChoices(outline); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				selected, selectedEvent := outline >= 0 && outline == trotterChoicePressIndex, trotterChoicePressEvent
				trotterChoicePressActive, trotterChoicePressIndex = false, -1
				if selected {
					if err := startTrotterEventResponse(selectedEvent); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
				if trotterChoiceOutline >= 0 {
					if err := drawTrotterChoices(-1); err != nil {
						return render.IndexedFrame{}, false, err
					}
					return currentFrame, true, nil
				}
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
					dogVisibleState = false
					nativeLoops.Stop(scripts.LoopKindActor, "dog")
					if activeSetName == "town" && view.Resource == 135 && worldPoint[2] == assets.SetDirectionNorth {
						boneInInventory, boneOwner, boneWorldProp.Visible = false, "none", false
						inventoryOwners["bone"], handItem = "none", ""
						dog2OfferStage, pendingSpotMovie = dog2OfferMovie, "MOVIES/DOG2.MOV"
						if *debug {
							log.Printf("actor=dog offerobject=Bone visible=false point=%v phase=%d movie=DOG2.MOV", worldPoint, gamePhase)
						}
					} else if *debug {
						log.Printf("actor=dog offerobject=Bone putdown-only set=%s view=%s direction=%d", activeSetName, view.Name[1:], worldPoint[2])
					}
					if err := refreshWorldScene(); err != nil {
						return render.IndexedFrame{}, false, fmt.Errorf("refresh after Dog offer: %w", err)
					}
					return currentFrame, true, nil
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

// scriptPresenter adapts main's conversation closures to the GameHost.
type scriptPresenter struct {
	open     func(file string) error
	close    func() error
	speak    func(line string) (bool, error)
	speaking func() bool
	skipped  func() bool
	choose   func([]scripts.PuppetChoice) error
	chosen   func() (int32, bool)
	show     func(layer string) error
	cursor   func(name string) error
}

func (p *scriptPresenter) OpenPuppet(file string) error              { return p.open(file) }
func (p *scriptPresenter) ClosePuppet() error                         { return p.close() }
func (p *scriptPresenter) Speak(line string) (bool, error)            { return p.speak(line) }
func (p *scriptPresenter) Speaking() bool                             { return p.speaking() }
func (p *scriptPresenter) Skipped() bool                              { return p.skipped() }
func (p *scriptPresenter) Choose(choices []scripts.PuppetChoice) error { return p.choose(choices) }
func (p *scriptPresenter) Chosen() (int32, bool)                      { return p.chosen() }
func (p *scriptPresenter) Show(layer string) error                    { return p.show(layer) }
func (p *scriptPresenter) Cursor(name string) error                   { return p.cursor(name) }

// mainScriptFallback handles the flat messages the port routes to existing
// code until flats run through the interpreter: mainpanel's tiphat.
type mainScriptFallback struct {
	tiphat func() error
}

func (f *mainScriptFallback) Command(call *scripts.ScriptCall) (int, uint16, error) {
	kind := call.Program.Records[call.Start].Kind
	if scripts.CommandHandlerName(kind) == "sendtoflat" {
		target, message, consumed, status, err := call.SendParts()
		if err != nil || status != 0 {
			return 0, status, err
		}
		if strings.EqualFold(target.Text, "mainpanel") && strings.EqualFold(message, "tiphat") {
			return consumed, 0, f.tiphat()
		}
		return 0, 0, fmt.Errorf("%w: sendtoflat(%q, %s())", scripts.ErrHostOpcodeUnimplemented, target.Text, message)
	}
	return 0, 0, fmt.Errorf("%w: %s (%d)", scripts.ErrHostOpcodeUnimplemented, scripts.CommandHandlerName(kind), kind)
}

func (f *mainScriptFallback) Value(call *scripts.ScriptCall) (scripts.Record, int, uint16, error) {
	kind := call.Program.Records[call.Start].Kind
	return scripts.Record{}, 0, 0, fmt.Errorf("%w: %s (%d)", scripts.ErrHostOpcodeUnimplemented, scripts.CommandHandlerName(kind), kind)
}
