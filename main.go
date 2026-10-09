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
	"redust/scripts/native"
	"redust/scripts/story"
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

// nativeMoviePath resolves a movie name a script gives playmovie against the
// movies directory BOOTFILE sets with path(3, "dust:movies:").
func nativeMoviePath(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if strings.Contains(name, "/") {
		return name
	}
	return "MOVIES/" + strings.ToUpper(name)
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
	nativePumpCount := uint32(0)
	resetNativeRandom := func() {
		seed := scripts.NativeRandomSeed(native.NativeTickMilliseconds())
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
	var themeTrack *audio.OpenTrack
	var movieActionFrameOne bool
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
	tracks := audio.NewTracks(workspace, audioContext)
	if soundBank != nil {
		tracks.Adopt("DATA/UNILIB.SND", soundBank)
	} else if openErr := tracks.OpenTrack("unilib.snd"); openErr != nil && *debug {
		log.Printf("script tracks: unilib: %v", openErr)
	}
	defer tracks.Close()
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
	gameClock, gameDay, phase := story.NativeAdvanceClockFields(2, 1, 0)
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
	actorPoses := map[string]string{"leroy": "stand", "dog": "stand", "jones": "stand", "buick": "stand", "marie": "stand"}
	
	actorHeadings := map[string]int16{"leroy": 0, "dog": 32, "jones": 0, "buick": 0, "marie": 128}
	helpPhase := int16(0)
	// Script-managed actors are driven by the general interpreter over their
	// shipped scripts instead of hand-written state. The table holds every cast
	// actor, built by the native constructor (FUN_0040C1F0); only the managed
	// ones are drawn from it and have their loops dispatched through it, while
	// the rest keep their existing hand-coded paths for now.
	scriptActors := scripts.NewScriptActors()
	scriptActorCasts := map[string]assets.Cast{}
	scriptActorRecords := map[string]assets.CastActor{}
	scriptManaged := map[string]bool{"mwife": true, "blood": true, "buick": true, "marie": true, "jones": true, "leroy": true, "help": true, "laurel": true, "trotter": true, "isao": true, "dog": true}
	var pendingMovement assets.SceneMove
	var scriptTask *scripts.ScriptTask
	var startScriptTask func(event scripts.ActorEvent) error
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
	// Every cast actor runs from its shipped scripts.
	for key := range scriptActorRecords {
		scriptManaged[key] = true
	}
	const townCastWalkRate int16 = 3
	const townActorHotDistance = 384
	leroyPhase := int16(0)

	// A choice base used to be captured from `currentFrame` on entry, so the choice screen
	// inherited whatever was last on screen. That is what put an animated puppet's own pixels
	// under the standing pose and made the figure appear twice, so **there is no choice base
	// any more**: each choice screen builds its own from the conversation base, which is scene
	// only, plus the standing pose it draws itself.
	jonesPhase, laurelPhase := int16(0), int16(0)
	jonesRingStory := int16(0)
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	
	isaoPhase, trotterPhase, oonaActorValue := int16(0), int16(0), int32(0)
	var bloodPhase, docPhase, fightOn int16
	var fearPhase int16
	hotelSavedScene, hotelSavedDirection := "Scene C4", "east"
	var hotelSceneOpenAction *story.HotelSceneOpenAction
	hotelSceneOpenStep, hotelEventsLocked := 0, false
	var pendingHotelActorSetup, pendingHotelActorSelector string
	var hotelVoiceSteps []story.HotelVoiceStep
	var hotelVoiceIndex int
	var hotelVoice *engine.VoiceOne
	var hotelVoiceBank *audio.SoundBank
	var hotelDelayUntil uint32
	var hotelDoorAfterDelay string
	var hotelHotplateStage *assets.Stage
	var hotelHotplateBank *audio.SoundBank
	var hotelHotplatePending story.HotelAction
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
	var finishPlayerDeath func(string) error
	var deathSequence story.DeathSequence
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

	mariePhase := int16(0)
	handItem, handFlag, inventoryMenuActive := "", int16(0), false
	var marieInventoryProjected []render.ProjectedFlatProp
	var inventoryCashRendered int32
	var inventoryCashRenderedValid bool
	boneWorldProp := render.WorldPropSprite{Name: "Bone", Set: "town", Position: bonePosition, Heading: 32, Scale: 1200, Archive: inventoryArchive, View: boneSmallView}
	boneOwner := "none"
	var boneInventoryFrame render.PuppetFrame
	var boneInInventory bool
	inventoryOwners := map[string]string{"gun": "none", "boots": "none", "bullets": "none", "badge": "none", "hankerchief": "none", "hhkey": "none", "hairpin": "none", "ring": "none", "bone": "none"}
	inventoryHidden := make(map[string]bool)
	inventoryDegrees := make(map[string]int16)
	var boneDragging bool
	var boneDragLast image.Point
	var heldItemDragging bool
	var heldItemDragFrame render.PuppetFrame
	var heldItemDragName string
	var heldItemDragLast image.Point
	defer func() {
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
		
		// No cell cull is applied here. Two attempts were made and both were wrong: the
		// first required the actor to be in the cell exactly one step ahead, which hid
		// every actor in the street until the player walked into their cell, and the second
		// admitted anything ahead in the facing direction, which did not hold up either.
		// The reference's own rule has not been read out of the binary yet, and guessing it
		// cost a regression. See assets.ActorCellVisible, which is kept as the measured
		// cell arithmetic but is deliberately not wired in.
		if activeSetName == "sallower" {
			return actors, nil
		}
		if activeSetName != "town" {
			return actors, nil
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
	scriptHost := &scripts.GameHost{Actors: scriptActors, Props: scripts.NewScriptProps(), Loops: &nativeLoops, Random: &nativeRandom, Env: scripts.GameHostEnv{
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
		ResolvePath: func(set, star, actorStar string, position [3]int16) (*assets.Path, bool) {
			source := nightSet
			if strings.EqualFold(set, activeSetName) {
				source = activeSet
			} else if !strings.EqualFold(set, "town") {
				return nil, false
			}
			var path *assets.Path
			var found bool
			var err error
			if strings.EqualFold(actorStar, "resume") {
				path, found, err = source.FindPathTo(star, position)
			} else {
				path, found, err = source.FindPath(star, actorStar)
			}
			if err != nil {
				log.Printf("walk path %s -> %s: %v", actorStar, star, err)
				return nil, false
			}
			return path, found
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
		Heading: render.NativeActorHeadingToPoint,
		// frame() is DAT_00459998, which FUN_004059D0 advances once per pump (20 a
		// second), not the 60 Hz tick counter.
		Frame:     func() int32 { return int32(nativePumpCount) },
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
	scriptHost.Env.SceneMove = func(code int) { pendingMovement = assets.SceneMove(code) }
	scriptHost.Env.Instanced = func(source, name string) {
		from, to := strings.ToLower(source), strings.ToLower(name)
		scriptActorCasts[to] = scriptActorCasts[from]
		record := scriptActorRecords[from]
		record.Name = name
		scriptActorRecords[to] = record
		if scriptManaged[from] {
			scriptManaged[to] = true
		}
	}
	scriptHost.Env.Diagnose = func(message string) {
		if *debug {
			log.Printf("script-note %s", message)
		}
	}
	scriptHost.Env.SceneCell = func(name string) (int16, int16, bool) {
		if activeSet == nil {
			return 0, 0, false
		}
		for _, sceneView := range activeSet.Views() {
			if strings.EqualFold(string(sceneView.Name[1:]), name) {
				return int16(sceneView.DirectionID)*256 + 128, int16(sceneView.SceneID)*256 + 128, true
			}
		}
		return 0, 0, false
	}
	scriptHost.Env.SceneAtCell = func(row, col int16) (string, bool) {
		if activeSet == nil {
			return "", false
		}
		for _, sceneView := range activeSet.Views() {
			if int16(sceneView.SceneID) == row && int16(sceneView.DirectionID) == col {
				return string(sceneView.Name[1:]), true
			}
		}
		return "", false
	}
	scriptHost.Env.SceneBuild = func(name string) (bool, bool) {
		if activeSet == nil {
			return false, false
		}
		for _, sceneView := range activeSet.Views() {
			if strings.EqualFold(string(sceneView.Name[1:]), name) {
				return sceneView.Flags != 0, true
			}
		}
		return false, false
	}
	scriptHost.Env.SceneChain = func(name string) ([]scripts.ScriptFrame, bool, error) {
		if activeSet == nil {
			return nil, false, nil
		}
		for _, sceneView := range activeSet.Views() {
			if !strings.EqualFold(string(sceneView.Name[1:]), name) {
				continue
			}
			me := strings.ToLower(name)
			var chain []scripts.ScriptFrame
			// FUN_0041A1A0 builds the chain from the view's own script (a
			// walkable cell's resource), the script of its area (the table at
			// metadata offset 0x1B8C) and the set script.
			if sceneView.IsCell() {
				program, err := scriptProgram(activeSet.File(), sceneView.Resource)
				if err != nil {
					return nil, false, err
				}
				chain = append(chain, scripts.ScriptFrame{Program: program, Me: me, Target: me, Label: "Scene Script: "})
			}
			if resource, ok := activeSet.SceneScriptResource(sceneView.ReferenceIndex); ok {
				program, err := scriptProgram(activeSet.File(), resource)
				if err != nil {
					return nil, false, err
				}
				chain = append(chain, scripts.ScriptFrame{Program: program, Me: me, Target: me, Label: "Area Script: "})
			}
			program, err := scriptProgram(activeSet.File(), activeSet.ScriptResource())
			if err != nil {
				return nil, false, err
			}
			chain = append(chain, scripts.ScriptFrame{Program: program, Me: me, Target: me, Label: "Set Script: ", Last: true})
			return chain, true, nil
		}
		return nil, false, nil
	}
	scriptInterpreter.Builtins = map[string]func(*scripts.ScriptCall) (int, uint16, error){}
	var scriptBlackFrame render.IndexedFrame
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
		{"leroyphase", func() int32 { return int32(leroyPhase) }, func(v int32) { leroyPhase = int16(v) }},
		{"jonesphase", func() int32 { return int32(jonesPhase) }, func(v int32) { jonesPhase = int16(v) }},
		{"jonesringstory", func() int32 { return int32(jonesRingStory) }, func(v int32) { jonesRingStory = int16(v) }},
		{"laurelphase", func() int32 { return int32(laurelPhase) }, func(v int32) { laurelPhase = int16(v) }},
		{"helpphase", func() int32 { return int32(helpPhase) }, func(v int32) { helpPhase = int16(v) }},
		{"isaophase", func() int32 { return int32(isaoPhase) }, func(v int32) { isaoPhase = int16(v) }},
		{"fighton", func() int32 { return int32(fightOn) }, func(v int32) { fightOn = int16(v) }},
	}
	publishBone := func() {
		prop := scriptHost.Props.Get("bone")
		prop.Owner, prop.Visible, prop.View, prop.Set = boneOwner, boneWorldProp.Visible, boneWorldProp.View.Name, boneWorldProp.Set
		prop.X, prop.Y, prop.Z = int32(boneWorldProp.Position[0]), int32(boneWorldProp.Position[1]), int32(boneWorldProp.Position[2])
		prop.Degree, prop.Scale, prop.ZClip = boneWorldProp.Heading, int32(boneWorldProp.Scale), int32(boneWorldProp.ZClip)
	}
	publishBone()
	publishScriptGlobals := func() error {
		for name, owner := range inventoryOwners {
			prop := scriptHost.Props.Get(name)
			prop.Owner = owner
		}
		for name, degree := range inventoryDegrees {
			scriptHost.Props.Get(name).Degree = degree
		}
		if oona, status := scriptActors.Lookup("oona"); status == 0 {
			oona.Value = oonaActorValue
		}
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
		if _, declared, err := scriptInterpreter.GlobalValue("debugging"); err != nil {
			return err
		} else if !declared {
			// BootFile's boot() sets it false; the port does not run boot().
			if err := scriptInterpreter.SetGlobalBool("debugging", false); err != nil {
				return err
			}
		}
		if _, declared, err := scriptInterpreter.GlobalValue("isrepeat"); err != nil {
			return err
		} else if !declared {
			// BootFile's boot() sets it false; keyrepeat sets it around keydown.
			if err := scriptInterpreter.SetGlobalBool("isrepeat", false); err != nil {
				return err
			}
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
		if dog, status := scriptActors.Lookup("dog"); status == 0 {
			dogVisibleState = dog.Visible
		}
		for _, name := range scriptHost.Props.Names() {
			prop := scriptHost.Props.Get(name)
			if previous, tracked := inventoryOwners[name]; tracked && previous != prop.Owner || !tracked && prop.Owner != "none" {
				inventoryOwners[name] = prop.Owner
			}
			if previous, tracked := inventoryDegrees[name]; tracked && previous != prop.Degree || !tracked && prop.Degree != 0 {
				inventoryDegrees[name] = prop.Degree
			}
		}
		bone := scriptHost.Props.Get("bone")
		if bone.View != boneWorldProp.View.Name {
			view, err := inventoryArchive.View("bone", bone.View)
			if err != nil {
				return err
			}
			boneWorldProp.View = view
		}
		boneOwner, boneInInventory = bone.Owner, strings.EqualFold(bone.Owner, "stranger")
		boneWorldProp.Set, boneWorldProp.Visible = bone.Set, bone.Visible
		boneWorldProp.Position = [3]int16{int16(bone.X), int16(bone.Y), int16(bone.Z)}
		boneWorldProp.Heading, boneWorldProp.Scale, boneWorldProp.ZClip = bone.Degree, int16(bone.Scale), int16(bone.ZClip)
		if boneInInventory {
			boneInventoryFrame = inventoryLargeFrames["bone"]
		}
		if value, declared, err := scriptInterpreter.GlobalValue("handitem"); err != nil {
			return err
		} else if declared && value.Kind == 3 {
			handItem = value.Text
		}
		for _, shared := range sharedPhases {
			if value, declared, err := scriptInterpreter.GlobalValue(shared.name); err != nil {
				return err
			} else if declared && value.Kind == 4 {
				shared.set(value.Int)
			}
		}
		if value, declared, err := scriptInterpreter.GlobalValue("phase"); err != nil {
			return err
		} else if declared && value.Kind == 4 && int(value.Int) != phase {
			if *debug {
				log.Printf("script-phase %d -> %d", phase, value.Int)
			}
			phase, gamePhase = int(value.Int), int16(value.Int)
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
	runScriptIn := func(label string, chain []scripts.ScriptFrame, source string) error {
		if err := publishScriptGlobals(); err != nil {
			return err
		}
		status, err := scriptInterpreter.RunSource(chain, source)
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
			log.Printf("script-status %s status=%#x record=%d site=%s", label, status, scriptInterpreter.ProgramCounter, scriptInterpreter.Site())
		} else if *debug && *debugLoops {
			log.Printf("script-run %s", label)
		}
		return nil
	}
	runScript := func(label, source string) error { return runScriptIn(label, nil, source) }
	// raiseSetEvent sends the set script (set) or the open scene's chain (scene)
	// one of openset(), closeset(), openscene() or closescene(), the way
	// FUN_00419D20, FUN_00419EE0, FUN_00419BC0 and FUN_00419C70 do for the sets
	// whose scripts the port runs. setEventsOpen is true between the set's open
	// and close events.
	scriptedSets := map[string]bool{"town": true}
	setEventsOpen := false
	var raiseSetEvent func(set bool, message string) error
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
		if err := runScript("initactors", `sendtocast("gang",initactors())`); err != nil {
			return err
		}
		return runScript("initactors extra", `sendtocast("extra",initactors())`)
	}
	// NEW.FLT initall runs the cast's initactors at the start of every day;
	// the first run happens here, before the opening scene.
	// The cast open sends every actor openactor() once, which is where the
	// extra cast's actors make their instances (actorinstance).
	for index := 1; index <= scriptActors.Count(); index++ {
		record, ok := scriptActors.At(index)
		if !ok {
			continue
		}
		name := record.Name
		if scriptManaged[strings.ToLower(name)] {
			if err := runScript("openactor "+name, fmt.Sprintf("sendtoactor(%q,openactor())", name)); err != nil {
				stage.Close()
				return err
			}
		}
	}
	if err := runInitActors(); err != nil {
		stage.Close()
		return err
	}
	// The cast's setups may have placed actors in the opening scene (Leroy
	// by the sign on Day 1), so draw it again.
	if worldActors, err = loadWorldActors(worldPoint); err != nil {
		stage.Close()
		return err
	}
	if worldBackground, projectedActors, err = compositeWorld(backgroundFrame, worldPoint, worldActors); err != nil {
		stage.Close()
		return fmt.Errorf("render G15 actors: %w", err)
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
		if !boneDragging && !heldItemDragging {
			item := strings.ToLower(handItem)
			if inventoryOwners[item] == "stranger" && !inventoryHidden[item] {
				itemFrame, found := inventoryLargeFrames[item]
				if !found {
					itemFrame, err = loadPropFrame(inventoryArchive, item, "large", 0, inventoryDegrees[item])
					if err != nil {
						return render.IndexedFrame{}, err
					}
					inventoryLargeFrames[item] = itemFrame
				}
				return render.CompositePuppetFrame(frame, itemFrame, image.Pt(316, 320))
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
	startAvatarNoFace(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
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
		avatarTipNextFrame = scripts.NativeFrameUnits(native.NativeTickMilliseconds()) + nativeAvatarFrameInterval
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
		if !avatarIdleActive {
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
		hotelSceneAction, openHotelScene := story.HotelOpenSceneAction(semanticName, nextView.Resource, story.HotelStoryState{Day: gameDay, Clock: gameClock, Phase: gamePhase, FearPhase: fearPhase, LaurelPhase: laurelPhase, InventoryOwners: inventoryOwners, SavedScene: hotelSavedScene, SavedDirection: hotelSavedDirection})
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
		trotterSet := ""
		if actor, status := scriptActors.Lookup("trotter"); status == 0 {
			trotterSet = actor.Set
		}
		trotterState := story.TrotterStoryState{Day: int16(gameDay), Clock: int16(gameClock), Phase: gamePhase, TrotterPhase: trotterPhase, TrotterActorSet: trotterSet, DocPhase: docPhase}
		closeTrotter := story.NativeTrotterSetTransition(trotterState, previousName, false)
		if closeTrotter.Hide {
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: "trotter", Message: "putdownactor()"}); err != nil {
				return render.IndexedFrame{}, err
			}
		}
		if setEventsOpen {
			// FUN_00419EE0: closeset(), then closefloor(), then closescene().
			if err := raiseSetEvent(true, "closeset()"); err != nil {
				return render.IndexedFrame{}, err
			}
			if err := raiseSetEvent(false, "closescene()"); err != nil {
				return render.IndexedFrame{}, err
			}
			setEventsOpen = false
		}
		if previousName == "town" && semanticName != "town" {
			townReturnScene = string(view.Name[1:])
			for _, owner := range []string{"dog", "isao"} {
				nativeLoops.Stop(scripts.LoopKindActor, owner)
			}
			
		}
		if previousName == "hotlower" && semanticName != "hotlower" {
			if actor, status := scriptActors.Lookup("laurel"); status == 0 && strings.EqualFold(actor.Set, "hotlower") {
				if err := deliverScriptEvent(scripts.ActorEvent{Actor: "laurel", Message: "putdownactor()"}); err != nil { return render.IndexedFrame{}, err }
			}
		}
		activeSet, activeSetName, activeSetOwned = nextSet, semanticName, nextOwned
		render.SetNativeCamera(nextSet.CameraPullback(), nextSet.CameraHeight())
		view, worldPoint, backgroundFrame = nextView, nextPoint, background
		scriptHost.OpenSet()
		doorOwner = ""
		if scriptedSets[semanticName] {
			// FUN_00419D20: openset(), then openfloor(), then openscene().
			setEventsOpen = true
			if err := raiseSetEvent(true, "openset()"); err != nil {
				return render.IndexedFrame{}, err
			}
			if err := raiseSetEvent(false, "openscene()"); err != nil {
				return render.IndexedFrame{}, err
			}
		}
		if previousName == "hotupper" && semanticName != "hotupper" {
			nativeLoops.Stop(scripts.LoopKindScene, "scene c4")
		}
		if semanticName == "hotupper" && pendingHotelActorSetup != "" && scriptManaged[strings.ToLower(pendingHotelActorSetup)] {
			// HOTROOM's door sends the day's hallway actor its own setup.
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: pendingHotelActorSetup, Message: fmt.Sprintf("setupactor(%q)", pendingHotelActorSelector)}); err != nil {
				return render.IndexedFrame{}, err
			}
			pendingHotelActorSetup, pendingHotelActorSelector = "", ""
		}
		openTrotter := story.NativeTrotterSetTransition(trotterState, semanticName, true)
		if openTrotter.Setup != "" {
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: "trotter", Message: fmt.Sprintf("setupactor(%q)", openTrotter.Setup)}); err != nil {
				return render.IndexedFrame{}, err
			}
		}
		if openTrotter.Hide {
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: "trotter", Message: "putdownactor()"}); err != nil {
				return render.IndexedFrame{}, err
			}
		}
		if openTrotter.SetGamePhaseValid {
			gamePhase = openTrotter.SetGamePhase
		}
		if semanticName == "sallower" {
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: "isao", Message: "setupactor(\"bar\")"}); err != nil {
				return render.IndexedFrame{}, err
			}
		} else if err := deliverScriptEvent(scripts.ActorEvent{Actor: "isao", Message: "putdownactor()"}); err != nil {
			return render.IndexedFrame{}, err
		}
		if semanticName == "hotlower" && (gameDay == 1 && laurelPhase < 2 && gamePhase < 7 || gameDay == 2 && (gamePhase == 1 && gameClock == 1 || gameClock == 3 && gamePhase == 0) || gameDay == 3 && gameClock == 2 && laurelPhase == 0) {
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: "laurel", Message: "setupactor(\"hotel\")"}); err != nil { return render.IndexedFrame{}, err }
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
			if gameDay == 1 && dogVisibleState {
				if err := deliverScriptEvent(scripts.ActorEvent{Actor: "dog", Message: "doright()"}); err != nil {
					return render.IndexedFrame{}, err
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
	
	// A script conversation: the open puppet, its base, the line playing and
	// the choice panel, driven by the GameHost through scriptPresenter.
	var scriptTaskDone func()
	// Set once the movie, transition and day-advance machinery below exists.
	var scriptEngineBusy func() bool
	var scriptPlayMovie func(name string) error
	// scriptCursorName is the last cursor(name) a script chose, which the hover
	// code reads after it runs a scene's setcursor.
	var scriptCursorName string
	var scriptAdvanceDay func() error
	scriptMovieFinished, scriptActionFrameOne := false, false
	var convPuppet *render.Puppet
	var convTable assets.PuppetSpeechTable
	var convName string
	var convBase render.IndexedFrame
	var convDialogue *engine.PuppetDialogue
	var convSkipped, convChoosing, convHasChoice bool
	scriptInventorySeen, convDirty := false, false
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
		currentFrame, stageFrame, convDirty = frame, frame, true
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
			frame, err := dialogue.Start(convBase, scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
			if err != nil {
				return false, err
			}
			convDialogue, convSkipped, convChoosing = dialogue, false, false
			currentFrame, stageFrame, convDirty = frame, frame, true
			if *debug {
				log.Printf("script-puppet speak=%s", line)
			}
			return true, nil
		},
		speaking: func() bool { return convDialogue != nil && convDialogue.Active() },
		skipped:  func() bool { return convSkipped },
		choose: func(choices []scripts.PuppetChoice) error {
			if len(choices) == 0 {
				// A puppetevent timeout withdraws the panel.
				convChoosing, convHasChoice = false, false
				return nil
			}
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
			if layer == "stage" {
				return nil
			}
			if layer == "black" {
				convDirty = true
				currentFrame, stageFrame = scriptBlackFrame, scriptBlackFrame
				return nil
			}
			convDirty = true
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
		cursor:   func(name string) error { scriptCursorName = name; return nil },
		gotoFlat: func(name string) error { return nil },
		play: func(name string) error {
			scriptMovieFinished = false
			return scriptPlayMovie(name)
		},
		movie: func() bool { return scriptMovieFinished && !scriptEngineBusy() },
		pick: func() error {
			scriptInventorySeen, convDirty = false, true
			if currentScene != 2 {
				return enterInventoryScene()
			}
			return nil
		},
		picked: func() bool {
			if currentScene == 2 {
				scriptInventorySeen = true
				return false
			}
			if scriptInventorySeen {
				scriptInventorySeen = false
				return true
			}
			return false
		},
	}
	scriptHost.Env.Sync = publishScriptGlobals
	scriptHost.Env.HaltTheme = func() {
		if themePlayer != nil {
			if err := themePlayer.Close(); err != nil {
				log.Printf("halttheme: %v", err)
			}
			themePlayer = nil
		}
	}
	scriptHost.Env.Tracks = tracks
	tracks.OnClose = func(track *audio.OpenTrack) {
		// FUN_0040E650 releases the file's sounds, which stops its theme.
		if themeTrack == track {
			scriptHost.Env.HaltTheme()
			themeTrack, currentThemeName = nil, ""
		}
	}
	scriptHost.Env.PlayTheme = func(name string) {
		// FUN_0040E6F0 finds the open track file the theme belongs to.
		if themePlayer != nil || audioContext == nil || strings.EqualFold(name, "None") {
			return
		}
		theme, track := currentTheme, themeTrack
		if found := tracks.Find(name); found != nil {
			loaded, err := found.Bank.LoadTheme(found.Name)
			if err != nil {
				log.Printf("playtheme %s: %v", name, err)
				return
			}
			theme, track = loaded, found
		}
		if theme.Name == "" {
			return
		}
		player, err := theme.Play(audioContext)
		if err != nil {
			log.Printf("playtheme: %v", err)
			return
		}
		themePlayer, currentTheme, themeTrack, currentThemeName = player, theme, track, theme.FirstVoiceName()
		if *debug {
			log.Printf("script-theme %s events=%d voices=%d channel-name=%s", theme.Name, len(theme.Events), len(theme.Tracks), currentThemeName)
		}
	}
	scriptHost.Env.Voice = func(name string) error {
		if found, err := tracks.Play(name, true); err != nil || found {
			return err
		}
		return scriptHost.Env.Sound(name)
	}
	scriptHost.Env.SoundVolume = func(name string, level int) { tracks.SetVolume(name, level) }
	scriptHost.Env.ActionFrame = func(n int) (bool, bool) { return scriptActionFrameOne, n == 1 }
	scriptHost.Env.Busy = func() bool { return scriptEngineBusy != nil && scriptEngineBusy() }
	scriptHost.Env.OpenSetFile = func(name string) error {
		frame, err := switchSpecialSet(name, "", "")
		if err != nil {
			return err
		}
		currentFrame, stageFrame, convDirty = frame, frame, true
		return nil
	}
	scriptHost.Env.StageScript = func(string) (*scripts.Program, error) {
		return scriptProgram("DATA/NEW.FLT", 1)
	}
	scriptHost.Env.Theme = func() string {
		if themePlayer == nil {
			return ""
		}
		return currentThemeName
	}
	scriptHost.Env.ThemeName = func() string {
		if themePlayer == nil {
			return ""
		}
		return currentTheme.Name
	}
	scriptHost.Env.BootScript = func() (*scripts.Program, error) {
		return scriptProgram("BootFile", 1)
	}
	scriptHost.Env.AdvanceDay = func() error {
		if scriptAdvanceDay == nil {
			return fmt.Errorf("day advance is not ready")
		}
		return scriptAdvanceDay()
	}
	scriptInterpreter.Builtins["advanceday"] = scriptHost.AdvanceDayBuiltin
	directionNames := map[int16]string{assets.SetDirectionNorth: "north", assets.SetDirectionSouth: "south", assets.SetDirectionEast: "east", assets.SetDirectionWest: "west"}
	scriptHost.Env.View = func() (string, string) {
		return strings.ToLower(string(view.Name[1:])), directionNames[worldPoint[2]]
	}
	scriptHost.Env.SetView = func(scene, direction string) error {
		heading := worldPoint[2]
		for value, name := range directionNames {
			if name == direction {
				heading = value
			}
		}
		// FUN_004199A0 does nothing when the player is already in that cell and
		// otherwise sends closescene() before the move and openscene() after it.
		moved := view.Name[0] != 0 && !strings.EqualFold(string(view.Name[1:]), scene)
		if moved {
			if err := raiseSetEvent(false, "closescene()"); err != nil {
				return err
			}
		}
		if _, err := setWorldView(scene, heading); err != nil {
			return err
		}
		if err := refreshWorldScene(); err != nil {
			return err
		}
		if moved {
			return raiseSetEvent(false, "openscene()")
		}
		return nil
	}
	scriptInterpreter.Builtins["handleselect"] = scriptHost.PickInventoryBuiltin
	var trackBanks [2]*audio.SoundBank
	scriptHost.Env.Sound = func(name string) error {
		if found, err := tracks.Play(name, false); err != nil || found {
			return err
		}
		if soundBank == nil || audioContext == nil {
			return nil
		}
		// Named sounds live in the library the script opened: the universal
		// bank, or the night/town track file for the ambient animals.
		for _, bank := range []*audio.SoundBank{soundBank, themeBank} {
			if bank.Has(name) {
				return bank.Play(audioContext, name, 1)
			}
		}
		for index, file := range []string{"DATA/TOWN.SND", "DATA/NIGHT.SND"} {
			if trackBanks[index] == nil {
				if bank, err := audio.OpenSoundBank(workspace, file); err == nil {
					trackBanks[index] = bank
				}
			}
			if trackBanks[index].Has(name) {
				return trackBanks[index].Play(audioContext, name, 1)
			}
		}
		if *debug {
			log.Printf("script-note sound %q is in no open bank", name)
		}
		return nil
	}
	scriptHost.Env.PuppetFile = workspace.OpenPuppetFile
	scriptHost.Env.Program = scriptProgram
	scriptHost.Env.Shop = func(name string) (string, uint32, bool) {
		// INVEN.PRP is the inventory shop the NEW.FLT boot opens for the
		// whole game; its shop script is resource 0 +0x924.
		// BOOTFILE opens house.prp and inven.prp (openshopfile) for the whole game.
		file := ""
		switch strings.ToLower(name) {
		case "inven":
			file = "DATA/INVEN.PRP"
		case "house":
			file = "DATA/HOUSE.PRP"
		default:
			return "", 0, false
		}
		data, err := scriptResource(file, 0)
		if err != nil || len(data) < 0x928 {
			return "", 0, false
		}
		return file, binary.LittleEndian.Uint32(data[0x924:0x928]), true
	}
	scriptHost.Env.PropDegree = func(name string) (int16, bool) {
		return inventoryDegrees[strings.ToLower(name)], true
	}
	// propinstance (FUN_0041E840) adds props that share their source's script.
	propInstances := map[string]string{}
	scriptHost.Env.PropInstance = func(source, name string) (uint16, bool) {
		key, sourceKey := strings.ToLower(name), strings.ToLower(source)
		if _, exists := propInstances[key]; exists {
			return 0, false
		}
		if original, aliased := propInstances[sourceKey]; aliased {
			sourceKey = original
		}
		if _, found := inventoryArchive.Definition(sourceKey); !found {
			if _, found := propArchive.Definition(sourceKey); !found {
				return 0x0a, false
			}
		}
		if _, found := inventoryArchive.Definition(key); found {
			return 0, false
		}
		if _, found := propArchive.Definition(key); found {
			return 0, false
		}
		propInstances[key] = sourceKey
		return 0, true
	}
	scriptHost.Env.PropScript = func(name string) (string, uint32, string, bool) {
		if original, aliased := propInstances[strings.ToLower(name)]; aliased {
			name = original
		}
		if definition, found := inventoryArchive.Definition(name); found {
			return "DATA/INVEN.PRP", definition.ScriptResource, "inven", true
		}
		definition, found := propArchive.Definition(name)
		return "DATA/HOUSE.PRP", definition.ScriptResource, "house", found
	}
	// FUN_004204D0: opening a shop file sends each of its props openprop().
	for _, archive := range []*assets.PropArchive{propArchive, inventoryArchive} {
		for _, name := range archive.Names() {
			if err := runScript("openprop "+name, fmt.Sprintf("sendtoprop(%q,openprop())", name)); err != nil {
				stage.Close()
				return err
			}
		}
	}
	scriptHost.Env.Ticks = func() uint32 { return scripts.NativeFrameUnits(native.NativeTickMilliseconds()) }
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
	}, death: func() error {
		cause, _, err := scriptInterpreter.GlobalValue("playerdeath")
		if err != nil {
			return err
		}
		return finishPlayerDeath(cause.Text)
	}}
	// startScriptTask runs an event that may block (a conversation) as a
	// task the update loop resumes.
	startScriptSourceIn := func(label string, chain []scripts.ScriptFrame, source string, done func()) error {
		if scriptTask != nil {
			return nil
		}
		scriptTaskDone = done
		task := scripts.StartScriptTask(label, func(task *scripts.ScriptTask) error {
			scriptHost.SetTask(task)
			return runScriptIn(label, chain, source)
		})
		finished, err := task.Poll()
		if collectErr := collectScriptGlobals(); err == nil {
			err = collectErr
		}
		if finished {
			scriptHost.SetTask(nil)
			if done != nil {
				scriptTaskDone = nil
				done()
			}
			return err
		}
		scriptTask = task
		return nil
	}
	startScriptSource := func(label, source string, done func()) error {
		return startScriptSourceIn(label, nil, source, done)
	}
	raiseSetEvent = func(set bool, message string) error {
		if activeSet == nil || !scriptedSets[activeSetName] {
			return nil
		}
		var chain []scripts.ScriptFrame
		source, label := message, "set "+message
		if set {
			// FUN_0041A840 -> FUN_0041A920 runs a set message against the set
			// script alone.
			program, err := scriptProgram(activeSet.File(), activeSet.ScriptResource())
			if err != nil {
				return err
			}
			chain = []scripts.ScriptFrame{{Program: program, Me: "set", Target: "set", Label: "Set Script: ", Last: true}}
		} else {
			if view.Name[0] == 0 {
				return nil
			}
			scene := strings.ToLower(string(view.Name[1:]))
			source, label = fmt.Sprintf("sendtoscene(%q,%s)", scene, message), scene+" "+message
		}
		if scriptTask != nil {
			return runScriptIn(label, chain, source)
		}
		return startScriptSourceIn(label, chain, source, nil)
	}
	// BOOTFILE keydown and mousedown return at once while lockevents is true.
	eventsLocked := func() bool {
		if hotelEventsLocked {
			return true
		}
		value, declared, err := scriptInterpreter.GlobalValue("lockevents")
		return err == nil && declared && value.Kind == 2 && value.Int != 0
	}
	raiseSceneLoop := func(loop scripts.ScriptLoop) error {
		source, label := fmt.Sprintf("sendtoscene(%q,%s())", loop.Owner, loop.Callback), loop.Owner+" "+loop.Callback+"()"
		if scriptTask != nil {
			return runScriptIn(label, nil, source)
		}
		return startScriptSourceIn(label, nil, source, nil)
	}
	// raiseKey is BOOTFILE keydown for the world: sendtoscene(currentscene(),
	// keydown(arg)). It reports whether the scene chain took the key.
	raiseKey := func(name string) bool {
		if !scriptedSets[activeSetName] || !setEventsOpen || view.Name[0] == 0 {
			return false
		}
		scene := strings.ToLower(string(view.Name[1:]))
		if err := startScriptSource(scene+" keydown", fmt.Sprintf("sendtoscene(%q,keydown(%q))", scene, name), nil); err != nil {
			log.Printf("scene keydown: %v", err)
		}
		return true
	}
	// dispatchScriptEvent raises an engine event (a finished walk, a fired loop).
	// Scripts reached this way may block, the way the original's message pump lets
	// them: an actor that has stared at the player long enough starts a
	// conversation from its idle loop. With no task running the event becomes one;
	// while one is suspended the event runs inline and cannot block.
	var dispatchScriptEvent func(event scripts.ActorEvent) error
	startScriptTask = func(event scripts.ActorEvent) error {
		return startScriptSource(event.Actor+" "+event.Message, fmt.Sprintf("sendtoactor(%q,%s)", event.Actor, event.Message), nil)
	}
	dispatchScriptEvent = func(event scripts.ActorEvent) error {
		if scriptTask == nil {
			return startScriptTask(event)
		}
		return deliverScriptEvent(event)
	}
	schedulerPasses, schedulerWindow := 0, native.NativeTickMilliseconds()
	// The native pump (FUN_00406920) finishes each render by waiting until the
	// timer FUN_0042B700, which counts 60 units a second, has advanced by the
	// boot script's framerate(3). Walk jobs, loops and projection therefore run
	// 20 times a second, not once per host update.
	const nativePumpInterval uint32 = 3
	nextNativePump := uint32(0)
	forceNativePump := false
	nativePumpDue := func() bool {
		now := scripts.NativeFrameUnits(native.NativeTickMilliseconds())
		if int32(now-nextNativePump) < 0 {
			return false
		}
		nextNativePump = now + nativePumpInterval
		nativePumpCount++
		return true
	}
	runNativeScheduler := func(visualEffectPump bool) (bool, error) {
		displayChanged := false
		// Only the actor simulation (jobs, turns, loops) runs at the pump
		// rate; interaction state machines keep running every update.
		pumpDue := visualEffectPump || forceNativePump || nativePumpDue()
		forceNativePump = false
		if *debug && *debugLoops && pumpDue {
			schedulerPasses++
			if now := native.NativeTickMilliseconds(); now-schedulerWindow >= 1000 {
				log.Printf("scheduler passes=%d window-ms=%d", schedulerPasses, now-schedulerWindow)
				schedulerPasses, schedulerWindow = 0, now
			}
		}
		scriptsPaused := scriptTask != nil && !scriptHost.PassRequested()
		if !visualEffectPump && !scriptsPaused && pumpDue {
			// FUN_0040F4E0 runs the walk and turn jobs before the loops.
			events, moved := scriptHost.Pass()
			displayChanged = displayChanged || moved && currentScene == 0
			for _, event := range events {
				if err := dispatchScriptEvent(event); err != nil {
					return false, err
				}
			}
		}
		status, err := nativeLoops.PassWhere(func(loop scripts.ScriptLoop) (uint16, error) {
			if event, isActor := scripts.LoopEvent(loop); isActor && scriptManaged[strings.ToLower(loop.Owner)] {
				displayChanged = displayChanged || currentScene == 0
				return 0, dispatchScriptEvent(event)
			}
			switch loop.Callback {
			case "trigger":
				if hotelSceneOpenAction == nil || activeSetName != "hotupper" {
					hotelEventsLocked = false
					return 0, nil
				}
				for hotelSceneOpenStep < len(hotelSceneOpenAction.TriggerSteps) {
					step := hotelSceneOpenAction.TriggerSteps[hotelSceneOpenStep]
					switch step.Kind {
					case story.HotelTriggerSetPhase:
						gamePhase, phase = step.Phase, int(step.Phase)
					case story.HotelTriggerSetLockEvents:
						hotelEventsLocked = step.LockEvents
					case story.HotelTriggerRunPuppet:
						hotelSceneOpenStep++
						if scriptManaged[strings.ToLower(step.Actor)] {
							// HOTUPPER's trigger runs the cast's runpuppet, then resumes its
							// remaining steps when the conversation ends.
							resume := loop
							resume.Remaining = 1
							if *debug {
								log.Printf("hotel-open runpuppet actor=%s puppet=%s phase=%d", step.Actor, step.Puppet, gamePhase)
							}
							if err := startScriptSource("hotel "+step.Puppet, fmt.Sprintf("sendtocast(\"gang\",runpuppet(%q))", step.Puppet), func() {
								if hotelSceneOpenAction != nil && activeSetName == "hotupper" {
									nativeLoops.Register(resume)
								}
							}); err != nil {
								return 0, err
							}
							return 0, nil
						}
						if *debug {
							log.Printf("hotel-open continuation=%d kind=%d actor=%s puppet=%s pending=true", hotelSceneOpenStep, step.Kind, step.Actor, step.Puppet)
						}
						return 0, nil
					case story.HotelTriggerDelay:
						hotelSceneOpenStep++
						loop.Remaining = int32(step.DelayTicks)
						if *debug {
							log.Printf("hotel-open delay=%d next=%d", step.DelayTicks, hotelSceneOpenStep)
						}
						return nativeLoops.Register(loop), nil
					case story.HotelTriggerMoveActor:
						if !scriptManaged[strings.ToLower(step.Actor)] {
							return 0, fmt.Errorf("hotel trigger cannot move actor %q", step.Actor)
						}
						hotelSceneOpenStep++
						hotelSceneOpenAction, hotelEventsLocked = nil, false
						displayChanged = true
						if err := deliverScriptEvent(scripts.ActorEvent{Actor: step.Actor, Message: fmt.Sprintf("moveactor(%q)", step.Destination)}); err != nil {
							return 0, err
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
			
			default:
				if loop.Kind == scripts.LoopKindScene && scriptedSets[activeSetName] {
					// FUN_0040FB00 builds sendtoscene("<owner>", <callback>()) for a
					// scene loop and has already cleared its slot, so the callback
					// makes its own loop again.
					return 0, raiseSceneLoop(loop)
				}
				return 0, fmt.Errorf("unknown native loop callback %q", loop.Callback)
			}
		}, func(loop scripts.ScriptLoop) bool {
			if !pumpDue || scriptsPaused && loop.Kind == scripts.LoopKindActor && scriptManaged[strings.ToLower(loop.Owner)] {
				return false
			}
			return true
		})
		if err != nil {
			return false, err
		}
		if status != 0 {
			return false, fmt.Errorf("native scene scheduler returned status %#x", status)
		}
		
		if displayChanged {
			if err := refreshWorldScene(); err != nil {
				return false, err
			}
		}
		
		
		
		
		return displayChanged, nil
	}
	startSceneMovie := func(name string) error {
		movieName := nativeMoviePath(name)
		if currentScene == 0 {
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
		if deathStage == 3 || sceneMovieAfter != nil {
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
	var pendingPlayerMovement assets.SceneMove
	var pendingSceneMovie string
	nativeCursor := ""
	debugCursor := ""
	setNativeCursor := func(name string) {
		if *debug && debugCursor != name {
			debugCursor = name
			log.Printf("cursor=%s", name)
		}
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
	// sceneCursor is BOOTFILE idle() for the world: while events are locked the
	// cursor is "watch", otherwise the scene chain's setcursor(point) picks it.
	// The result is reused while the point and view stay put.
	var cursorKey string
	var cursorName string
	cursorAge := 0
	sceneCursor := func(point uint32, fallback string) string {
		if eventsLocked() {
			return "watch"
		}
		if view.Name[0] == 0 {
			return fallback
		}
		scene := strings.ToLower(string(view.Name[1:]))
		key := fmt.Sprintf("%s %d %d %d %d", scene, worldPoint[2], point, gameDay, gameClock)
		cursorAge++
		if key == cursorKey && cursorAge < 20 {
			return cursorName
		}
		cursorKey, cursorAge, scriptCursorName = key, 0, ""
		if err := runScriptIn(scene+" setcursor", nil, fmt.Sprintf("sendtoscene(%q,setcursor(%d))", scene, int32(point))); err != nil {
			log.Printf("scene setcursor: %v", err)
		}
		cursorName = fallback
		if scriptCursorName != "" {
			cursorName = scriptCursorName
		}
		return cursorName
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
			if scriptedSets[activeSetName] && setEventsOpen {
				if cursor != "touch" {
					cursor = sceneCursor(point, cursor)
				}
			} else if activeSetName == "sallower" {
				if _, found := story.SallowerDoorAt(view.Resource, worldPoint[2], point); found {
					cursor = "touch"
				}
			} else if activeSetName == "hotlower" {
				if _, found := story.HotLowerDoorAt(view.Resource, worldPoint[2], point); found {
					cursor = "touch"
				}
			}
			
			
			if inventoryMenuActive && currentScene == 2 {
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
	var readingAction story.InventoryExamination
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
	var flatMouseSession *story.FlatMouseSession
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
	// options runs the options flat's buttons; creditsScr is the credits
	// screen and quitRequested is quit() (FUN_00426580).
	var options optionsFlow
	var creditsScr creditsScreen
	var quitRequested bool
	// uiPending is a frame a key press produced for the dialogs; the next
	// update shows it, since the key callback itself returns no frame.
	var uiPending render.IndexedFrame
	var uiHasPending bool
	composeScoreVolume := func(base render.IndexedFrame, slider native.MenuVolumeSlider) (render.IndexedFrame, image.Rectangle, error) {
		sprites := append([]render.FlatPropSprite{{Name: "slider", PropName: "slider", ViewName: "BASE", Anchor: image.Pt(slider.X, slider.Y), Archive: propArchive}}, options.Sprites(propArchive)...)
		frame, projected, err := render.CompositeFlatProps(base, sprites)
		if err != nil {
			return render.IndexedFrame{}, image.Rectangle{}, fmt.Errorf("draw score volume slider: %w", err)
		}
		track := image.Rectangle{}
		for _, prop := range projected {
			if prop.Name == "slider" {
				track = image.Rect(prop.Bounds.Min.X, scoreMenuVolumeTop, prop.Bounds.Max.X, scoreMenuVolumeBottom+1)
			}
		}
		if track.Empty() {
			return render.IndexedFrame{}, image.Rectangle{}, fmt.Errorf("score volume slider is outside the frame")
		}
		if frame, err = options.DrawKeys(frame); err != nil {
			return render.IndexedFrame{}, image.Rectangle{}, fmt.Errorf("draw options keys: %w", err)
		}
		return frame, track, nil
	}
	setScoreMenuVolume := func(level int) (native.MenuVolumeSlider, bool, error) {
		slider, status, err := native.NativeMenuVolume(level, audio.SetWaveVolume)
		if err != nil {
			return native.MenuVolumeSlider{}, false, err
		}
		if status != 0 {
			return native.MenuVolumeSlider{}, false, fmt.Errorf("menu volume returned status %#x", status)
		}
		if stage == mainStage && currentScene == 3 && transition == nil && scoreVolumeBaseFrame.Width > 0 {
			frame, track, err := composeScoreVolume(scoreVolumeBaseFrame, slider)
			if err != nil {
				return native.MenuVolumeSlider{}, false, err
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
		positions := map[string][3]int16{"bone": boneWorldProp.Position}
		progress := save.GameProgress{Version: 1, Day: gameDay, Clock: gameClock, Phase: int16(phase), GamePhase: gamePhase, SetName: activeSetName, ViewName: string(view.Name[1:]), TownReturnScene: townReturnScene, Point: worldPoint, PlayerCash: playercash, InventoryOwners: cloneStringMap(inventoryOwners), InventoryHidden: cloneBoolMap(inventoryHidden), HandItem: handItem, HandFlag: handFlag, BoneOwner: boneOwner, BoneInInventory: boneInInventory, BoneWorldVisible: boneWorldProp.Visible, DogVisible: dogVisibleState, ActorPoses: cloneStringMap(actorPoses), ActorHeadings: actorHeadings, ActorPositions: positions, StoryValues: map[string]int32{"isaoPhase": int32(isaoPhase), "trotterPhase": int32(trotterPhase), "helpPhase": int32(helpPhase), "jonesPhase": int32(jonesPhase), "mariePhase": int32(mariePhase), "laurelPhase": int32(laurelPhase), "oonaActorValue": oonaActorValue}, StoryFlags: map[string]bool{}}
		progress.StoryValues["jonesRingStory"] = int32(jonesRingStory)
		progress.StoryValues["bloodPhase"], progress.StoryValues["docPhase"], progress.StoryValues["fightOn"] = int32(bloodPhase), int32(docPhase), int32(fightOn)
		progress.StoryValues["fearPhase"] = int32(fearPhase)
		
		progress.StoryStrings = map[string]string{"hotelSavedScene": hotelSavedScene, "hotelSavedDirection": hotelSavedDirection}
		progress.InventoryDegrees = make(map[string]int16, len(inventoryDegrees))
		for item, degree := range inventoryDegrees {
			progress.InventoryDegrees[item] = degree
		}
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
		progress.ActorHeadings = make(map[string]int16, len(actorHeadings))
		for name, heading := range actorHeadings {
			progress.ActorHeadings[name] = heading
		}
		params := scriptHost.PuppetParams
		progress.PuppetParams = &params
		return progress
	}
	initialProgress := captureGameProgress()
	initialProgress.ScriptLoops = nil
	// saveGameProgress is savegame(gameName) (FUN_00422C80 -> FUN_00422DE0):
	// it writes the .rtd container named fileName under the saves directory.
	saveGameProgress := func(gameName, fileName string) error {
		savePath, err := save.RTDPath(savesDirectory(workspace.WorkDir), fileName)
		if err != nil {
			return err
		}
		progress := captureGameProgress()
		openFiles := []string{"BOOTFILE", "UNILIB.SND", "GANG.CST", "EXTRA.CST", "HOUSE.PRP", "INVEN.PRP", "NEW.FLT"}
		if activeSetName != "" {
			openFiles = append(openFiles, strings.ToUpper(activeSetName)+".SET")
		}
		if err := save.SaveRTD(savePath, progress, save.RTDContext{GameName: gameName, PaletteRaw: mainStage.PaletteRaw, OpenFiles: openFiles}); err != nil {
			return err
		}
		if *debug {
			log.Printf("save=written path=%s day=%d clock=%d set=%s view=%s items=%d cash=%d", savePath, gameDay, gameClock, activeSetName, view.Name[1:], len(inventoryOwners), playercash)
		}
		return nil
	}
	applyGameProgress := func(progress save.GameProgress, savePath string) error {
		gameDay, gameClock, phase, gamePhase, playercash = progress.Day, progress.Clock, int(progress.Phase), progress.GamePhase, progress.PlayerCash
		if progress.PuppetParams != nil {
			scriptHost.PuppetParams = *progress.PuppetParams
		}
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
		if value, found := progress.StoryValues["isaoPhase"]; found {
			isaoPhase = int16(value)
		}
		if value, found := progress.StoryValues["trotterPhase"]; found {
			trotterPhase = int16(value)
		}
		bloodPhase, docPhase, fightOn = int16(progress.StoryValues["bloodPhase"]), int16(progress.StoryValues["docPhase"]), int16(progress.StoryValues["fightOn"])
		fearPhase = int16(progress.StoryValues["fearPhase"])
		
		
		
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
		if value, found := progress.StoryValues["helpPhase"]; found {
			helpPhase = int16(value)
		}
		if value, found := progress.StoryValues["jonesPhase"]; found {
			jonesPhase = int16(value)
		}
		jonesRingStory = int16(progress.StoryValues["jonesRingStory"])
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
		for name, heading := range progress.ActorHeadings {
			actorHeadings[name] = heading
		}
		for name, target := range map[string]*[3]int16{"bone": &boneWorldProp.Position} {
			if position, found := progress.ActorPositions[name]; found {
				*target = position
			}
		}
		worldPoint = progress.Point
		if err := scriptInterpreter.RestoreGlobals(progress.ScriptGlobals); err != nil {
			return err
		}
		if err := scriptActors.Restore(progress.ScriptActors); err != nil {
			return err
		}
		if progress.ScriptActors == nil {
			// A save from before script-managed actors (or a new game):
			// have the cast initialize them for the saved day, as initall does.
			// initactor() zeroes the shared story phases, which a legacy save
			// holds in its own fields, so put those back afterwards.
			savedPhases := make([]int32, len(sharedPhases))
			for index, shared := range sharedPhases {
				savedPhases[index] = shared.get()
			}
			if err := runInitActors(); err != nil {
				return err
			}
			for index, shared := range sharedPhases {
				shared.set(savedPhases[index])
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
		if _, migrated := progress.ScriptActors["laurel"]; !migrated {
			if visible, legacy := progress.StoryFlags["laurelVisible"]; legacy {
				if err := deliverScriptEvent(scripts.ActorEvent{Actor: "laurel", Message: "setupactor(\"hotel\")"}); err != nil { return err }
				actor, status := scriptActors.Lookup("laurel"); if status != 0 { return fmt.Errorf("legacy Laurel actor missing: %#x", status) }
				actor.Visible, actor.Value = visible, progress.StoryValues["laurelActorValue"]
				if position, found := progress.ActorPositions["laurel"]; found {
					actor.Position, actor.Placed = position, true
					if location, found := scriptHost.Env.ResolveStar("hotlower", "hotlower.laurel"); !found || position != location { actor.Star = "custom" }
				}
				if heading, found := progress.ActorHeadings["laurel"]; found { actor.Heading = heading }
				if pose, found := progress.ActorPoses["laurel"]; found && pose != "" { actor.Pose = pose }
				actor.Frame = 0
				if !visible { nativeLoops.Stop(scripts.LoopKindActor, "laurel"); actor.StopJob() }
				for old, name := range map[string]string{"laurelGood": "laurelgood", "laurelCounter": "counter", "oonakidstory": "oonakidstory"} {
					if value, found := progress.StoryValues[old]; found { if err := scriptInterpreter.SetGlobalNumber(name, value); err != nil { return err } }
				}
				if degree, found := progress.StoryValues["hankerchiefDegree"]; found { if _, owned := progress.InventoryDegrees["hankerchief"]; !owned { inventoryDegrees["hankerchief"] = int16(degree) } }
				if value, found := progress.StoryValues["laurelPhase"]; found { laurelPhase = int16(value) }
			}
		}
		if _, migrated := progress.ScriptActors["trotter"]; !migrated {
			if visible, legacy := progress.StoryFlags["trotterVisible"]; legacy {
				actor, status := scriptActors.Lookup("trotter")
				if status != 0 {
					return fmt.Errorf("legacy Trotter actor missing: %#x", status)
				}
				nativeLoops.Stop(scripts.LoopKindActor, "trotter")
				actor.StopJob()
				actor.Visible, actor.Placed, actor.Value, actor.Frame = visible, true, progress.StoryValues["trotterActorValue"], 0
				// The hand-coded actor started at the Sallowers bar with the
				// scale, speed and turn rate of its setup.
				actor.Set, actor.Star = "sallower", "sal.trotter1"
				actor.Scale, actor.Speed, actor.TurnRate, actor.ZClip = 4500, 4, 8, 32
				if set, found := progress.StoryStrings["trotterActorSet"]; found {
					actor.Set = set
				}
				if star, found := progress.StoryStrings["trotterStar"]; found {
					actor.Star = star
				}
				if position, found := progress.ActorPositions["trotter"]; found {
					actor.Position = position
				}
				if heading, found := progress.ActorHeadings["trotter"]; found {
					actor.Heading = heading
				}
				if pose, found := progress.ActorPoses["trotter"]; found && pose != "" {
					actor.Pose = pose
				}
				if value, found := progress.StoryValues["trotterScale"]; found {
					actor.Scale = value
				}
				if value, found := progress.StoryValues["trotterSpeed"]; found {
					actor.Speed = int16(value)
				}
				if value, found := progress.StoryValues["trotterTurnSpeed"]; found {
					actor.TurnRate = int16(value)
				}
				if value, found := progress.StoryValues["trotterZClip"]; found {
					actor.ZClip = int16(value)
				}
				if counter, found := progress.StoryValues["trotterGiftCounter"]; found {
					if err := scriptInterpreter.SetGlobalNumber("counter", counter); err != nil {
						return err
					}
				}
				if progress.TrotterWalk != nil {
					walk, err := scripts.RestoreNativeActorWalkJob(*progress.TrotterWalk)
					if err != nil {
						return err
					}
					target := "custom"
					for _, name := range []string{"sal.trotter1", "sal.trotter2", "sal.trotter9", "town.jones1", "town.jones3", "town.jones4", "town.jones5", "town.jones6", "town.trot1", "town.horse2", "doctor2.trot"} {
						if position, found := scriptHost.Env.ResolveStar(actor.Set, name); found && position == walk.Snapshot().Target {
							target = name
							break
						}
					}
					actor.Job = &scripts.ActorJob{Mode: scripts.ActorJobWalk, Target: target, Heading: walk.Snapshot().Heading, Walk: walk, TurnDone: true}
				}
			} else {
				// An older save carries no Trotter state; the initialisation above
				// cleared the placement the set-open step just made, so redo it.
				state := story.TrotterStoryState{Day: int16(gameDay), Clock: int16(gameClock), Phase: gamePhase, TrotterPhase: trotterPhase, DocPhase: docPhase}
				if open := story.NativeTrotterSetTransition(state, activeSetName, true); open.Setup != "" {
					if err := deliverScriptEvent(scripts.ActorEvent{Actor: "trotter", Message: fmt.Sprintf("setupactor(%q)", open.Setup)}); err != nil {
						return err
					}
				}
			}
		}
		if _, migrated := progress.ScriptActors["isao"]; !migrated {
			if visible, legacy := progress.StoryFlags["isaoVisible"]; legacy {
				actor, status := scriptActors.Lookup("isao")
				if status != 0 {
					return fmt.Errorf("legacy Isao actor missing: %#x", status)
				}
				nativeLoops.Stop(scripts.LoopKindActor, "isao")
				actor.StopJob()
				actor.Visible, actor.Placed, actor.Value, actor.Frame = visible, true, progress.StoryValues["isaoActorValue"], 0
				actor.Set, actor.Star = "sallower", "sallower.isao"
				actor.Scale, actor.Speed, actor.TurnRate, actor.ZClip = 4200, 4, 8, 32
				if position, found := progress.ActorPositions["isao"]; found {
					actor.Position = position
				}
				if heading, found := progress.ActorHeadings["isao"]; found {
					actor.Heading = heading
				}
				if pose, found := progress.ActorPoses["isao"]; found && pose != "" {
					actor.Pose = pose
				}
				for old, name := range map[string]string{"isaoBouncer": "bouncer", "isaoDirGo": "dirgo"} {
					value := int32(0)
					if progress.StoryFlags[old] {
						value = 1
					}
					if err := scriptInterpreter.SetGlobalNumber(name, value); err != nil {
						return err
					}
				}
				if counter, found := progress.StoryValues["isaoGiftCounter"]; found {
					if err := scriptInterpreter.SetGlobalNumber("counter", counter); err != nil {
						return err
					}
				}
			} else if activeSetName == "sallower" {
				// An older save carries no Isao state; the initialisation above
				// cleared the placement the set-open step just made.
				if err := deliverScriptEvent(scripts.ActorEvent{Actor: "isao", Message: "setupactor(\"bar\")"}); err != nil {
					return err
				}
			}
		}
		if _, migrated := progress.ScriptActors["dog"]; !migrated {
			// An older save keeps only whether the dog was out: put him at his
			// street post as initactors does on Day 1, or take him away.
			message := "putdownactor()"
			if progress.DogVisible {
				message = "setupactor(\"street\")"
			}
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: "dog", Message: message}); err != nil {
				return err
			}
		}
		if dog, status := scriptActors.Lookup("dog"); status == 0 {
			dogVisibleState = dog.Visible
		}
		delete(actorPoses, "laurel"); delete(actorHeadings, "laurel"); delete(actorPoses, "trotter"); delete(actorHeadings, "trotter"); delete(actorPoses, "isao"); delete(actorHeadings, "isao")
		currentScene, inventoryMenuActive = 0, false
		publishBone()
		startAvatarNoFace(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
		if err := refreshWorldScene(); err != nil {
			return err
		}
		if *debug {
			log.Printf("save=loaded path=%s day=%d clock=%d set=%s view=%s items=%d cash=%d", savePath, gameDay, gameClock, activeSetName, view.Name[1:], len(inventoryOwners), playercash)
		}
		return nil
	}
	loadGameProgressFile := func(savePath string) error {
		progress, err := save.LoadGameFile(savePath, "")
		if err != nil {
			return err
		}
		return applyGameProgress(progress, savePath)
	}
	// loadGameProgress is opengame(gameName) (FUN_00422D40 -> FUN_004235C0):
	// the stored name must match gameName before anything is restored.
	loadGameProgress := func(gameName, fileName string) error {
		savePath, err := save.RTDPath(savesDirectory(workspace.WorkDir), fileName)
		if err != nil {
			return err
		}
		progress, err := save.LoadGameFile(savePath, gameName)
		if err != nil {
			return err
		}
		return applyGameProgress(progress, savePath)
	}
	finishPlayerDeath = func(cause string) error {
		sequence, err := story.NewDeathSequence(workspace, cause, &nativeRandom)
		if err != nil {
			return err
		}
		deathSequence, deathStage, deathUntil = sequence, 1, scripts.NativeFrameUnits(native.NativeTickMilliseconds())+uint32(sequence.DelayFrames)
		avatarIdleActive, avatarGestureActive, avatarTipActive = false, false, false
		if *debug {
			log.Printf("playerdeath=%s movie=%s narration=%v", cause, sequence.Movie, sequence.Narration)
		}
		return nil
	}
	startDeathMovie := func() error {
		if setEventsOpen {
			if err := raiseSetEvent(true, "closeset()"); err != nil {
				return err
			}
			if err := raiseSetEvent(false, "closescene()"); err != nil {
				return err
			}
			setEventsOpen = false
		}
		nativeLoops = scripts.LoopScheduler{}
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
		deathUntil, deathStage = scripts.NativeFrameUnits(native.NativeTickMilliseconds())+uint32(duration), 5
		return nil
	}
	startNewGame := func() error {
		nativeLoops = scripts.LoopScheduler{}
		
		
		avatarTipActive = false
		deathStage = 0
		if err := applyGameProgress(initialProgress, "NEW.FLT/death/new"); err != nil {
			return err
		}

		// The set's own openset() starts its track file, theme and ambient loop.
		return refreshWorldScene()
	}
	hotelStoryState := func() story.HotelStoryState {
		return story.HotelStoryState{Day: gameDay, Clock: gameClock, Phase: gamePhase, FearPhase: fearPhase, LaurelPhase: laurelPhase, InventoryOwners: inventoryOwners, SavedScene: hotelSavedScene, SavedDirection: hotelSavedDirection}
	}
	var advanceDay func() (render.IndexedFrame, bool, error)
	advanceDay = func() (render.IndexedFrame, bool, error) {
		route, err := story.NativeAdvanceDayRoute(story.AdvanceDayState{Day: gameDay, Clock: gameClock, Phase: gamePhase, CurrentSet: activeSetName, InventoryOwners: inventoryOwners})
		if err != nil {
			return render.IndexedFrame{}, false, err
		}
		finish := func(bool) (render.IndexedFrame, bool, error) {
			gameDay, gameClock, gamePhase, phase = route.Day, route.Clock, route.Phase, int(route.Phase)
			dogVisibleState, trotterPhase = false, 0
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
			startAvatarNoFace(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
			defer func() {
				if err := runInitActors(); err != nil {
					log.Printf("initactors after advanceday: %v", err)
				}
			}()
			sceneName, direction := route.ViewName, route.Direction
			if sceneName == "" {
				name, nativeDirection, err := story.NativeSetInitialView(workspace, "DATA/"+strings.ToUpper(route.SetName))
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
				hotelDelayUntil = scripts.NativeFrameUnits(native.NativeTickMilliseconds()) + uint32(step.DelayFrames)
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
			if err := voice.Start(scripts.NativeFrameUnits(native.NativeTickMilliseconds())); err != nil {
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
	openHotelHotplate := func(action story.HotelAction) (render.IndexedFrame, error) {
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
			if err := deliverScriptEvent(scripts.ActorEvent{Actor: "laurel", Message: "putdownactor()"}); err != nil { return render.IndexedFrame{}, err }
		}
		for _, setup := range action.ActorSetups {
			if scriptManaged[strings.ToLower(setup.Name)] {
				if err := deliverScriptEvent(scripts.ActorEvent{Actor: setup.Name, Message: fmt.Sprintf("setupactor(%q)", setup.Selector)}); err != nil {
					return render.IndexedFrame{}, err
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
	runHotelAction := func(action story.HotelAction) (render.IndexedFrame, bool, error) {
		if action.Sound != "" && action.Kind != story.HotelActionHotplateChoice {
			bank := soundBank
			if hotelHotplateStage != nil && stage == hotelHotplateStage && hotelHotplateBank != nil {
				bank = hotelHotplateBank
			}
			if err := bank.Play(audioContext, action.Sound, 1); err != nil {
				return render.IndexedFrame{}, false, err
			}
		}
		switch action.Kind {
		case story.HotelActionDoor:
			if action.GiveItem != "" {
				inventoryOwners[action.GiveItem], inventoryHidden[action.GiveItem] = "stranger", false
				if err := soundBank.Play(audioContext, "inven", 1); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
			if action.DelayFrames > 0 {
				hotelDoorAfterDelay, hotelDelayUntil = action.DoorOwner, scripts.NativeFrameUnits(native.NativeTickMilliseconds())+uint32(action.DelayFrames)
			} else {
				doorOwner = action.DoorOwner
			}
		case story.HotelActionKnock:
			doorOwner = ""
		case story.HotelActionPuppet:
			if action.SetLaurelPhase { laurelPhase = action.LaurelPhase }
			if err := startScriptSource("hotel "+action.Puppet, fmt.Sprintf("sendtocast(\"gang\",runpuppet(%q))", strings.ToLower(strings.TrimPrefix(action.Puppet, "PUPPETS/"))), nil); err != nil { return render.IndexedFrame{}, false, err }
			return currentFrame, true, nil
		case story.HotelActionBreakfast:
			if playback != nil || transition != nil || sceneMovieAfter != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("HOTPLATE transition is already active")
			}
			hotelHotplatePending = action
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, action.FadeOutFrames)
			if err != nil {
				hotelHotplatePending = story.HotelAction{}
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode = fade, 9
			return fade.CurrentFrame(), true, nil
		case story.HotelActionHotplateChoice:
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
		case story.HotelActionHotplatePaper:
			returnFrame := currentFrame
			if err := startActionMovie(action.Movie, action.FadeOutFrames, func(bool) (render.IndexedFrame, bool, error) {
				return restoreActionFrame(returnFrame, action.FadeInFrames)
			}); err != nil {
				return render.IndexedFrame{}, false, err
			}
			return currentFrame, true, nil
		case story.HotelActionHotplateSound:
			return currentFrame, action.Sound != "", nil
		case story.HotelActionHotplateExit:
			if hotelHotplateStage == nil || stage != hotelHotplateStage || playback != nil || transition != nil {
				return currentFrame, false, nil
			}
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, action.FadeOutFrames)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			hotelHotplatePending, transition, transitionMode = action, fade, 10
			return fade.CurrentFrame(), true, nil
		case story.HotelActionFearVoices:
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
		case story.HotelActionSleep, story.HotelActionMovie:
			returnFrame := currentFrame
			if err := startActionMovie(action.Movie, action.FadeOutFrames, func(marker bool) (render.IndexedFrame, bool, error) {
				if action.Kind == story.HotelActionSleep && marker {
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
	queueHotelTransition := func(action story.HotelAction) error {
		pendingHotelActorSetup, pendingHotelActorSelector = action.ActorSetup, action.ActorSelector
		if action.SaveRoomReturn {
			hotelSavedScene, hotelSavedDirection = string(view.Name[1:]), "east"
		}
		sceneName, direction := action.ViewName, action.Direction
		if sceneName == "" {
			name, nativeDirection, err := story.NativeSetInitialView(workspace, "DATA/"+strings.ToUpper(action.SetName))
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
		action, found, err := story.ExamineInventoryItem(workspace, handItem, inventoryDegrees[strings.ToLower(handItem)])
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
		context := story.InventoryBookContext{FlatIndex: readingScene + 1, FlatCount: len(readingStage.Scenes), Point: point, BorderProp: readingAction.BorderProp, BorderDegree: readingAction.BorderDegree, PointInBorder: pointInBorder}
		var action story.InventoryBookAction
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
			action, found, err = story.ParseInventoryBookAction(program, context)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
		}
		if !found || action.DelegateStage {
			program, err := readProgram(1, readingStage)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			action, found, err = story.ParseInventoryBookAction(program, context)
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
		startAvatarNoFace(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
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
			if err := options.enter(stage, target); err != nil {
				return render.IndexedFrame{}, false, err
			}
			scoreVolumeBaseFrame = nextFrame
			slider, status, err := native.NativeMenuVolume(audio.WaveVolume(), func(int) error { return nil })
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
				startAvatarNoFace(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
			}
		} else if !mainFlat {
			stageFrame = nextFrame
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
				if effectName == "plain" {
					transitionMode = 2
				}
				return transition.CurrentFrame(), true, nil
			}
		}
		currentFrame = nextFrame
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
			choiceEvents := make([]int32, len(convChoices))
			engineBusy := scriptEngineBusy != nil && scriptEngineBusy()
			for index, choice := range convChoices {
				choiceEvents[index] = choice.EventID
			}
			globals := map[string]any{}
			if values, err := scriptInterpreter.SnapshotGlobals(); err == nil {
				for _, value := range values {
					if value.Kind == 3 {
						globals[value.Name] = value.Text
					} else {
						globals[value.Name] = value.Int
					}
				}
			} else {
				globals["snapshotError"] = err.Error()
			}
			return map[string]any{"day": gameDay, "clock": gameClock, "phase": gamePhase, "set": activeSetName, "scene": string(stage.Scenes[currentScene].Name[1:]), "point": worldPoint, "cash": playercash, "inventory": inventoryOwners, "inventoryMenu": inventoryMenuActive, "breakfast": hotelHotplateStage != nil && stage == hotelHotplateStage, "jonesPhase": jonesPhase, "jonesRingStory": jonesRingStory, "laurelPhase": laurelPhase, "helpPhase": helpPhase, "scriptGlobals": globals, "engineBusy": engineBusy, "bone": map[string]any{"visible": boneWorldProp.Visible, "owner": boneOwner, "position": boneWorldProp.Position, "view": boneWorldProp.View.Name}, "movieFrame": movieFrame, "movieWaiting": movieWaiting, "conversation": map[string]any{"open": convPuppet != nil, "choosing": convChoosing, "choiceEvents": choiceEvents, "taskActive": scriptTask != nil}, "options": map[string]any{"subtitles": scriptHost.PuppetParams[6] != 0, "seldir": options.menu.seldir, "keys": map[string]any{"north": options.key("north"), "east": options.key("east"), "west": options.key("west")}, "dialog": options.modal.Active(), "credits": creditsScr.Active(), "quit": quitRequested, "volume": audio.WaveVolume(), "lastSave": options.lastName}, "scriptActors": scriptActors.Snapshot([]string{"mwife", "blood", "buick", "marie", "jones", "leroy", "help", "laurel", "trotter", "isao", "dog"})}
		})
	}
	scriptBlackFrame = blackFrame
	scriptEngineBusy = func() bool {
		busy := currentScene == 2 || playback != nil || transition != nil || sceneMovieAfter != nil || pendingMovement != 0
		if busy && *debug && *debugLoops {
			log.Printf("script-busy scene=%d playback=%t transition=%t sceneMovieAfter=%t frame=%d waiting=%t", currentScene, playback != nil, transition != nil, sceneMovieAfter != nil, playback.FrameIndex(), playback.WaitingForInput())
		}
		return busy
	}
	scriptPlayMovie = func(name string) error {
		return startActionMovie(name, 0, func(actionOne bool) (render.IndexedFrame, bool, error) {
			scriptMovieFinished, scriptActionFrameOne = true, actionOne
			return blackFrame, true, nil
		})
	}
	scriptAdvanceDay = func() error {
		frame, changed, err := advanceDay()
		if err != nil {
			return err
		}
		if changed {
			currentFrame, stageFrame, convDirty = frame, frame, true
		}
		return nil
	}
	// The options flow's dialogs and the credits screen own the display and
	// freeze the game while they are up, as the native task-modal boxes do.
	options.env = optionsEnv{
		workDir: workspace.WorkDir,
		frame:   func() render.IndexedFrame { return currentFrame },
		recompose: func() (render.IndexedFrame, bool, error) {
			if stage != mainStage || currentScene != 3 || scoreVolumeBaseFrame.Width == 0 {
				return currentFrame, false, nil
			}
			slider, status, err := native.NativeMenuVolume(audio.WaveVolume(), func(int) error { return nil })
			if err != nil || status != 0 {
				return render.IndexedFrame{}, false, fmt.Errorf("read score volume slider: status=%#x err=%v", status, err)
			}
			frame, track, err := composeScoreVolume(scoreVolumeBaseFrame, slider)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			currentFrame, stageFrame, scoreVolumeTrack = frame, frame, track
			return frame, true, nil
		},
		flat: applyFlatTarget,
		returnToFlat: func(name string, source uint32) (render.IndexedFrame, bool, error) {
			resolved, status, err := scripts.ResolveGotoflatTarget(true, scripts.GotoflatValue{Type: 3, Name: name}, stageFlatLookup(stage))
			if err != nil || status != 0 {
				if *debug {
					log.Printf("gotoflat-return unresolved flat=%q status=%#x err=%v", name, status, err)
				}
				return currentFrame, false, nil
			}
			return applyFlatTarget(resolved-1, 0, 0, source)
		},
		saveGame: saveGameProgress,
		loadGame: loadGameProgress,
		playMovie: func(name string, after func() (render.IndexedFrame, bool, error)) error {
			// NEW.FLT r1 spotmovie: screentoblack("current",10), the movie, then
			// the screen fades back in over 30 frames.
			return startActionMovie(name, 10, func(bool) (render.IndexedFrame, bool, error) {
				frame, _, err := after()
				if err != nil {
					return render.IndexedFrame{}, false, err
				}
				return restoreActionFrame(frame, 30)
			})
		},
		getGlobal: func(name string) (string, bool) {
			value, declared, err := scriptInterpreter.GlobalValue(name)
			if err != nil || !declared || value.Kind != 3 {
				return "", false
			}
			return value.Text, true
		},
		setGlobal: scriptInterpreter.SetGlobalString,
		params:    &scriptHost.PuppetParams,
		quit:      func() { quitRequested = true },
		logf: func(format string, args ...any) {
			if *debug {
				log.Printf(format, args...)
			}
		},
	}
	// FUN_004172C0 initialises puppetparam 2..8 to 0x80, 0xFA, 0xFB, 0x378, 0x0C, 0
	// and 0; slot 7 (subtitles) starts off.
	scriptHost.PuppetParams = [8]int16{0, 0x80, 0xfa, 0xfb, 0x378, 0x0c, 0, 0}
	creditsScr.env = creditsEnv{
		workspace:    workspace,
		audioContext: audioContext,
		random:       nativeRandom.Inclusive,
		pauseTheme: func() {
			if themePlayer != nil {
				themePlayer.Pause()
			}
		},
		resumeTheme: func() {
			if themePlayer != nil {
				themePlayer.Play()
			}
		},
		restore: options.env.recompose,
		logf:    options.env.logf,
	}
	engine.TextInput = options.modal.HandleText
	engine.SubtitlesEnabled = func() bool { return scriptHost.PuppetParams[6] != 0 }
	runErr := runGame(initialFrame, func() (render.IndexedFrame, bool, error) {
		if quitRequested && !*silent {
			return render.IndexedFrame{}, false, ebiten.Termination
		}
		if uiHasPending {
			uiHasPending = false
			return uiPending, true, nil
		}
		if creditsScr.Active() {
			return creditsScr.Update()
		}
		if options.modal.Active() {
			return options.modal.Update()
		}
		displayChanged := false
		drainMovieAudioTails()
		if scriptTask != nil {
			if convDirty {
				convDirty = false
				return currentFrame, true, nil
			}
			if convDialogue != nil && convDialogue.Active() {
				frame, changed, err := convDialogue.Update(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
				if err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("advance script conversation: %w", err)
				}
				if changed {
					currentFrame, stageFrame, displayChanged = frame, frame, true
				}
			}
			if scriptHost.PassRequested() && nativePumpDue() {
				forceNativePump = true
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
			if collectErr := collectScriptGlobals(); err == nil {
				err = collectErr
			}
			if convDirty {
				convDirty, displayChanged = false, true
			}
			if done {
				label := scriptTask.Label
				scriptTask = nil
				scriptHost.SetTask(nil)
				if after := scriptTaskDone; after != nil {
					scriptTaskDone = nil
					defer after()
				}
				if scriptHost.PuppetOpen() {
					// A script stopped between openpuppetfile and closepuppetfile.
					log.Printf("script-task %s left a puppet open; closing it", label)
					if abortErr := scriptHost.AbortPuppet(); abortErr != nil {
						return render.IndexedFrame{}, false, abortErr
					}
					if refreshErr := refreshWorldScene(); refreshErr != nil {
						return render.IndexedFrame{}, false, refreshErr
					}
				}
				if err != nil {
					log.Printf("script-task %s: %v", label, err)
				} else if *debug {
					log.Printf("script-task %s complete", label)
				}
				displayChanged = true
			}
			if scriptTask == nil || !scriptEngineBusy() {
				return currentFrame, displayChanged, nil
			}
		}
		if hotelVoice != nil {
			done, err := hotelVoice.Update(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
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
		if hotelDelayUntil != 0 && scripts.NativeFrameUnits(native.NativeTickMilliseconds()) >= hotelDelayUntil {
			hotelDelayUntil = 0
			if hotelDoorAfterDelay != "" {
				doorOwner, hotelDoorAfterDelay = hotelDoorAfterDelay, ""
			} else if len(hotelVoiceSteps) > 0 {
				if err := startHotelVoiceStep(); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
		}
		if deathStage == 1 && scripts.NativeFrameUnits(native.NativeTickMilliseconds()) >= deathUntil {
			fade, err := render.NewFadeEffect(currentFrame, blackFrame, deathSequence.FadeOutFrames)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode, deathStage = fade, 5, 2
			return fade.CurrentFrame(), true, nil
		}
		if deathStage == 5 {
			finished := deathNarration != nil && !deathNarration.IsPlaying() || *silent && scripts.NativeFrameUnits(native.NativeTickMilliseconds()) >= deathUntil
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
			avatarChanged, err := advanceAvatarAnimation(scripts.NativeFrameUnits(native.NativeTickMilliseconds()))
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("advance player portrait: %w", err)
			}
			displayChanged = displayChanged || avatarChanged
			changed, err := runNativeScheduler(false)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if currentScene == 2 && inventoryMenuActive && inventoryCashNeedsRefresh(inventoryCashRendered, inventoryCashRenderedValid, playercash) {
				if err := renderMarieInventory(); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("refresh inventory cash: %w", err)
				}
				return currentFrame, true, nil
			}
			displayChanged = displayChanged || changed
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
			// FUN_004199A0 sends closescene() when a movement starts and
			// FUN_00406570 sends openscene() when it ends (the port moves at once).
			if err := raiseSetEvent(false, "closescene()"); err != nil {
				return render.IndexedFrame{}, false, err
			}
			if hasEventView {
				view = nextView
			} else {
				view = assets.SetView{}
			}
			worldPoint, backgroundFrame, stageFrame, currentFrame = nextPoint, nextBackground, nextFrame, nextFrame
			worldActors, projectedActors = nextActors, nextProjectedActors
			if err := raiseSetEvent(false, "openscene()"); err != nil {
				return render.IndexedFrame{}, false, err
			}
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
					hotelHotplatePending = story.HotelAction{}
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
					hotelHotplatePending = story.HotelAction{}
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
		if sceneMovieAfter != nil {
			movieActionFrameOne, err = playback.ActionFrame(1)
			if err != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("read movie actionframe(1): %w", err)
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
			return after(movieActionFrameOne)
		}
		if deathStage == 3 {
			fade, err := render.NewFadeEffect(blackFrame, deathFrame, deathSequence.FadeInFrames)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			transition, transitionMode, deathStage = fade, 6, 4
			return fade.CurrentFrame(), true, nil
		}
		if *debug {
			log.Printf("startup movies complete; scene=%s", stage.Scenes[currentScene].Name[1:])
		}
		if !setEventsOpen && currentScene == 0 && scriptedSets[activeSetName] {
			resetNativeRandom()
			setEventsOpen = true
			if err := raiseSetEvent(true, "openset()"); err != nil {
				return render.IndexedFrame{}, false, err
			}
			if err := raiseSetEvent(false, "openscene()"); err != nil {
				return render.IndexedFrame{}, false, err
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
		if options.modal.Active() {
			frame, changed, err := options.modal.HandleKey(key)
			if err != nil {
				log.Printf("dialog key: %v", err)
			} else if changed {
				uiPending, uiHasPending = frame, true
			}
			return
		}
		if creditsScr.Active() {
			return
		}
		if stage == mainStage && currentScene == 3 && transition == nil && playback == nil {
			// NEW.FLT r11 keydown: the options flat takes the key for the
			// selected key box.
			frame, changed, err := options.KeyDown(key)
			if err != nil {
				log.Printf("options key: %v", err)
			} else if changed {
				uiPending, uiHasPending = frame, true
			}
			return
		}
		if scriptTask != nil && key == ebiten.KeySpace {
			// Space skips the line being spoken, like a click.
			if convDialogue != nil && convDialogue.Active() {
				if frame, _, err := convDialogue.Skip(); err != nil {
					log.Printf("skip script conversation line: %v", err)
				} else {
					convSkipped, currentFrame, stageFrame, convDirty = true, frame, frame, true
				}
				return
			}
			if playback == nil {
				return
			}
		}
		if eventsLocked() {
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
		
		
		
		if pendingSceneMovie != "" {
			return
		}
		if playback == nil {
			if currentScene == 0 {
				switch options.MovementKey(key) {
				case ebiten.KeyArrowUp, ebiten.KeyW:
					if raiseKey("uparrow") {
						return
					}
					if action, found := story.HotelForwardAction(activeSetName, view.Resource, worldPoint[2], doorOwner, hotelStoryState()); found {
						if err := queueHotelTransition(action); err != nil {
							log.Printf("hotel transition: %v", err)
						}
						return
					}
					direction := ""
					switch activeSetName {
					case "sallower":
						if story.SallowerExitToTown(worldPoint[2], doorOwner) {
							direction = "east"
						}
					case "hotlower":
						direction, _ = story.HotLowerExitToTown(view.Resource, worldPoint[2], doorOwner)
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
					pendingPlayerMovement = assets.SceneMoveStraight
				case ebiten.KeyArrowDown, ebiten.KeyS:
					pendingPlayerMovement = assets.SceneMoveBackwards
				case ebiten.KeyArrowLeft, ebiten.KeyA:
					if raiseKey("leftarrow") {
						return
					}
					pendingPlayerMovement = assets.SceneMoveLeft
				case ebiten.KeyArrowRight, ebiten.KeyD:
					if raiseKey("rightarrow") {
						return
					}
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
		if creditsScr.Active() {
			if mouseEvent.Button == ebiten.MouseButtonLeft {
				return creditsScr.Click()
			}
			return currentFrame, false, nil
		}
		if options.modal.Active() {
			if mouseEvent.Button != ebiten.MouseButtonLeft {
				return currentFrame, false, nil
			}
			return options.modal.HandleMouseDown(mouseEvent.Point)
		}
		if scriptTask != nil && !scriptEngineBusy() {
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
		if eventsLocked() {
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
		if stage == mainStage && currentScene == 3 && mouseEvent.Button == ebiten.MouseButtonLeft {
			if level, found := scoreMenuVolumeAt(point, scoreVolumeTrack); found {
				volumeSliderDragging = true
				if _, _, err := setScoreMenuVolume(level); err != nil {
					return render.IndexedFrame{}, false, fmt.Errorf("set score menu volume: %w", err)
				}
				return currentFrame, true, nil
			}
		}
		if scriptTask != nil && currentScene == 2 && mouseEvent.Button == ebiten.MouseButtonLeft {
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
			item := strings.ToLower(handItem)
			if item != "bone" && inventoryOwners[item] == "stranger" && !inventoryHidden[item] && mouseEvent.Button == ebiten.MouseButtonLeft {
				if itemFrame, found := inventoryLargeFrames[item]; found {
					mousePoint := image.Pt(int(int16(point>>16)), int(int16(point)))
					hitItem, err := render.HitTestPuppetFrame(itemFrame, image.Pt(316, 320), mousePoint)
					if err != nil {
						return render.IndexedFrame{}, false, err
					}
					if hitItem {
						heldItemDragging, heldItemDragFrame, heldItemDragName, heldItemDragLast = true, itemFrame, item, mousePoint
						if err := refreshWorldScene(); err != nil {
							return render.IndexedFrame{}, false, err
						}
						dragFrame, err := render.CompositePuppetFrame(currentFrame, itemFrame, mousePoint)
						if err != nil {
							return render.IndexedFrame{}, false, err
						}
						currentFrame, stageFrame = dragFrame, dragFrame
						return currentFrame, true, nil
					}
				}
			}
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
			if mouseEvent.Button == ebiten.MouseButtonLeft {
				if action, found := story.HotelMouseAction(activeSetName, view.Resource, worldPoint[2], point, hotelStoryState()); found {
					return runHotelAction(action)
				}
			}
			if mouseEvent.Button == ebiten.MouseButtonLeft && activeSetName == "sallower" {
				if owner, found := story.SallowerDoorAt(view.Resource, worldPoint[2], point); found {
					doorOwner = owner
					if *debug {
						log.Printf("door=%s owner=door set=sallower direction=%d", owner, worldPoint[2])
					}
					return currentFrame, false, nil
				}
			} else if mouseEvent.Button == ebiten.MouseButtonLeft && activeSetName == "hotlower" {
				if owner, found := story.HotLowerDoorAt(view.Resource, worldPoint[2], point); found {
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
				
				if strings.EqualFold(actorName, "Bone") && boneWorldProp.Visible && boneOwner == "none" {
					camera := render.NativeActorCameraPosition(worldPoint)
					playerPosition := [3]int16{int16(camera[0]), int16(camera[1]), int16(camera[2])}
					distance := scripts.NativeActorDistance2D(boneWorldProp.Position, playerPosition)
					if distance < 512 {
						boneInventoryFrame = inventoryLargeFrames["bone"]
						boneInInventory, boneOwner, inventoryOwners["bone"], handItem, boneWorldProp.View, boneWorldProp.Visible = true, "stranger", "stranger", "Bone", boneLargeView, false
						publishBone()
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
			}
			if mouseEvent.Button == ebiten.MouseButtonLeft && scriptedSets[activeSetName] && setEventsOpen && view.Name[0] != 0 {
				// BOOTFILE mousedown: hittest found no actor or button, so the
				// click goes to the scene as sendtoscene(<scene>, mousedown(<point>)).
				scene := strings.ToLower(string(view.Name[1:]))
				if err := startScriptSource(scene+" mousedown", fmt.Sprintf("sendtoscene(%q,mousedown(%d))", scene, int32(point)), nil); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
		}
		if !hit {
			if hotelHotplateStage != nil && stage == hotelHotplateStage && mouseEvent.Button == ebiten.MouseButtonLeft {
				if action, found := story.HotelHotplateBackgroundAction(string(stage.Scenes[currentScene].Name[1:])); found {
					return runHotelAction(action)
				}
			}
			return render.IndexedFrame{}, false, nil
		}
		if hotelHotplateStage != nil && stage == hotelHotplateStage && mouseEvent.Button == ebiten.MouseButtonLeft {
			if action, found := story.HotelHotplateMouseAction(handler.ScriptResource, hotelStoryState()); found {
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
			action, found, err := story.ParseDeathButtonAction(program, string(handler.Name[1:]), *debug)
			if err != nil {
				return render.IndexedFrame{}, false, err
			}
			if found && action.Kind == story.DeathButtonNew {
				if err := startNewGame(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return currentFrame, true, nil
			}
			if found && action.Kind == story.DeathButtonHelp {
				if err := startSceneMovie(action.Movie); err != nil {
					return render.IndexedFrame{}, false, err
				}
				return playback.CurrentFrame(), true, nil
			}
			if found && action.Kind == story.DeathButtonQuit && *debug {
				return currentFrame, false, ebiten.Termination
			}
		}
		if stage == mainStage && currentScene == 3 && mouseEvent.Button == ebiten.MouseButtonLeft {
			optionsAction, optionsFound, optionsErr := story.ParseOptionsAction(program, string(handler.Name[1:]))
			if optionsErr != nil {
				return render.IndexedFrame{}, false, fmt.Errorf("parse options button script resource %d: %w", handler.ScriptResource, optionsErr)
			}
			if optionsFound {
				if optionsAction.Kind == story.OptionsActionCredits {
					return creditsScr.Start(currentFrame)
				}
				return options.Click(string(handler.Name[1:]))
			}
		}
		continuation, trackButton, err := story.ParseFlatMouseContinuation(program, string(handler.Name[1:]))
		if err != nil {
			return render.IndexedFrame{}, false, fmt.Errorf("parse flat button script resource %d: %w", handler.ScriptResource, err)
		}
		if trackButton {
			flatMouseResource = handler.ScriptResource
			flatMouseReturnFlat = currentFlatName(stage, currentScene)
			flatMouseSession = new(story.FlatMouseSession)
			*flatMouseSession = story.NewFlatMouseSession(string(handler.Name[1:]), point, continuation)
			if stage == mainStage && currentScene == 3 {
				// NEW.FLT r11 trackbut(): the button's bevel shows while held.
				return options.SetBevel(string(handler.Name[1:]))
			}
			return currentFrame, false, nil
		}
		action, found, err := native.MouseDownFlatAction(program)
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
		if options.modal.Active() {
			return options.modal.HandleMouseState(state)
		}
		if creditsScr.Active() {
			return currentFrame, false, nil
		}
		if scriptTask != nil && !scriptEngineBusy() {
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
			scoreFlat := stage == mainStage && currentScene == 3
			if status == story.FlatMousePending {
				if scoreFlat {
					name := flatMouseSession.Target
					if !inside {
						name = ""
					}
					return options.SetBevel(name)
				}
				return currentFrame, false, nil
			}
			continuation, resumed := flatMouseSession.Resume()
			returnFlatName := flatMouseReturnFlat
			resourceSource := flatMouseResource
			flatMouseSession, flatMouseResource, flatMouseReturnFlat = nil, 0, ""
			if scoreFlat {
				// trackbut() hides its bevel before the handler goes on.
				options.menu.bevel = ""
				if _, _, err := options.env.recompose(); err != nil {
					return render.IndexedFrame{}, false, err
				}
			}
			if status == story.FlatMouseCancel || !resumed {
				return currentFrame, scoreFlat, nil
			}
			switch continuation.Action {
			case story.FlatMouseActionExamineInventory:
				return openInventoryExamination()
			case story.FlatMouseActionSaveGame:
				// NEW.FLT r24: gotoflat(1), savegame(name), gotoflat(<the flat that
				// currentflat() returned>); the flat name resolves through
				// FUN_00411AD0's string path.
				if !continuation.ReturnToCurrentFlat {
					returnFlatName = ""
				}
				return options.Save(continuation.GameName, returnFlatName, resourceSource)
			case story.FlatMouseActionOpenGame:
				// NEW.FLT r25: opengame(name); the load rebuilds the scene itself.
				return options.Open(continuation.GameName)
			case story.FlatMouseActionQuit:
				return options.Quit(continuation.Prompts, continuation.GameName)
			case story.FlatMouseActionHelp:
				return options.Help(continuation.Movie)
			case story.FlatMouseActionGoToFlat:
				if continuation.FlatTarget == 0 {
					return returnToMainPanel(continuation.VisualEffect, continuation.Duration)
				}
			}
			return currentFrame, false, nil
		}
		
		if heldItemDragging {
			point := image.Pt(int(int16(state.Point>>16)), int(int16(state.Point)))
			if state.LeftDown {
				if point == heldItemDragLast {
					return currentFrame, false, nil
				}
				heldItemDragLast = point
				if err := refreshWorldScene(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				dragFrame, err := render.CompositePuppetFrame(currentFrame, heldItemDragFrame, point)
				if err != nil {
					return render.IndexedFrame{}, false, err
				}
				currentFrame, stageFrame = dragFrame, dragFrame
				return currentFrame, true, nil
			}
			if state.LeftReleased {
				heldItemDragging = false
				actor, hit := render.HitTestWorldActors(projectedActors, point)
				if err := refreshWorldScene(); err != nil {
					return render.IndexedFrame{}, false, err
				}
				if hit && scriptManaged[strings.ToLower(actor)] {
					if err := startScriptTask(scripts.ActorEvent{Actor: actor, Message: fmt.Sprintf("offerobject(%q)", heldItemDragName)}); err != nil {
						return render.IndexedFrame{}, false, err
					}
				}
				return currentFrame, true, nil
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
				if hit && scriptManaged[strings.ToLower(actorName)] {
					if err := refreshWorldScene(); err != nil {
						return render.IndexedFrame{}, false, err
					}
					if err := startScriptTask(scripts.ActorEvent{Actor: actorName, Message: `offerobject("bone")`}); err != nil {
						return render.IndexedFrame{}, false, err
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
	gotoFlat func(name string) error
	pick     func() error
	picked   func() bool
	play     func(name string) error
	movie    func() bool
}

func (p *scriptPresenter) OpenPuppet(file string) error                { return p.open(file) }
func (p *scriptPresenter) ClosePuppet() error                          { return p.close() }
func (p *scriptPresenter) Speak(line string) (bool, error)             { return p.speak(line) }
func (p *scriptPresenter) Speaking() bool                              { return p.speaking() }
func (p *scriptPresenter) Skipped() bool                               { return p.skipped() }
func (p *scriptPresenter) Choose(choices []scripts.PuppetChoice) error { return p.choose(choices) }
func (p *scriptPresenter) Chosen() (int32, bool)                       { return p.chosen() }
func (p *scriptPresenter) Show(layer string) error                     { return p.show(layer) }
func (p *scriptPresenter) Cursor(name string) error                    { return p.cursor(name) }
func (p *scriptPresenter) GotoFlat(name string) error                  { return p.gotoFlat(name) }
func (p *scriptPresenter) PlayMovie(name string) error                 { return p.play(name) }
func (p *scriptPresenter) MovieDone() bool                             { return p.movie() }
func (p *scriptPresenter) PickInventory() error                        { return p.pick() }
func (p *scriptPresenter) Picked() bool                                { return p.picked() }

// mainScriptFallback handles the flat messages the port routes to existing
// code until flats run through the interpreter: mainpanel's tiphat.
type mainScriptFallback struct {
	tiphat func() error
	death  func() error
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
		if strings.EqualFold(target.Text, "death") && strings.EqualFold(message, "death") {
			return consumed, 0, f.death()
		}
		return 0, 0, fmt.Errorf("%w: sendtoflat(%q, %s())", scripts.ErrHostOpcodeUnimplemented, target.Text, message)
	}
	return 0, 0, fmt.Errorf("%w: %s (%d)", scripts.ErrHostOpcodeUnimplemented, scripts.CommandHandlerName(kind), kind)
}

func (f *mainScriptFallback) Value(call *scripts.ScriptCall) (scripts.Record, int, uint16, error) {
	kind := call.Program.Records[call.Start].Kind
	return scripts.Record{}, 0, 0, fmt.Errorf("%w: %s (%d)", scripts.ErrHostOpcodeUnimplemented, scripts.CommandHandlerName(kind), kind)
}
