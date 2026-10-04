package save

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"redust/scripts"
)

type GameProgress struct {
	Version              uint16                        `json:"version"`
	Day                  int                           `json:"day"`
	Clock                int                           `json:"clock"`
	Phase                int16                         `json:"phase"`
	GamePhase            int16                         `json:"gamePhase"`
	SetName              string                        `json:"set"`
	ViewName             string                        `json:"view"`
	TownReturnScene      string                        `json:"townReturnScene,omitempty"`
	Point                [3]int16                      `json:"point"`
	PlayerCash           int32                         `json:"playerCash"`
	InventoryOwners      map[string]string             `json:"inventoryOwners"`
	InventoryHidden      map[string]bool               `json:"inventoryHidden"`
	HandItem             string                        `json:"handItem,omitempty"`
	HandFlag             int16                         `json:"handFlag"`
	BoneOwner            string                        `json:"boneOwner"`
	BoneInInventory      bool                          `json:"boneInInventory"`
	BoneWorldVisible     bool                          `json:"boneWorldVisible"`
	DogVisible           bool                          `json:"dogVisible"`
	ActorPoses           map[string]string             `json:"actorPoses"`
	ActorHeadings        map[string]int16              `json:"actorHeadings"`
	ActorPositions       map[string][3]int16           `json:"actorPositions"`
	StoryValues          map[string]int32              `json:"storyValues"`
	StoryFlags           map[string]bool               `json:"storyFlags"`
	StoryStrings         map[string]string             `json:"storyStrings,omitempty"`
	InventoryDegrees     map[string]int16              `json:"inventoryDegrees,omitempty"`
	ScriptLoops          *scripts.LoopSchedulerState   `json:"scriptLoops,omitempty"`
	TrotterWalk          *scripts.NativeActorWalkState `json:"trotterWalk,omitempty"`
	TrotterWalkFrame     int                           `json:"trotterWalkFrame,omitempty"`
	TrotterWalkRemaining uint32                        `json:"trotterWalkRemaining,omitempty"`
	NativeLoopKinds      bool                          `json:"nativeLoopKinds,omitempty"` // loop kinds follow FUN_004112A0; older saves used actor 2, scene 1

	// ScriptActors holds the interpreter-managed actors' records.
	ScriptActors map[string]scripts.ActorRecordState `json:"scriptActors,omitempty"`
	// ScriptGlobals holds the interpreter's global variables.
	ScriptGlobals []scripts.GlobalVariable `json:"scriptGlobals,omitempty"`
}

func GameProgressPath(directory, gameName string) (string, error) {
	gameName = strings.TrimSpace(gameName)
	if gameName == "" || gameName == "." || gameName == ".." || strings.ContainsAny(gameName, `/\\`) {
		return "", fmt.Errorf("game save name %q is invalid", gameName)
	}
	return filepath.Join(directory, gameName+".redust.json"), nil
}

func ValidateGameProgress(state GameProgress) error {
	if state.Version != 1 {
		return fmt.Errorf("game save version %d is unsupported", state.Version)
	}
	if state.Day < 1 || state.Day > 5 || state.Clock < 1 || state.Clock > 3 || state.Point[2] < 1 || state.Point[2] > 4 {
		return fmt.Errorf("game save time/position is invalid: day=%d clock=%d point=%v", state.Day, state.Clock, state.Point)
	}
	if state.SetName == "" || state.ViewName == "" || state.PlayerCash < 0 {
		return fmt.Errorf("game save set/view/cash state is invalid")
	}
	if state.ScriptLoops != nil {
		if err := state.ScriptLoops.Validate(); err != nil {
			return fmt.Errorf("game save loops: %w", err)
		}
	}
	if state.TrotterWalkFrame < 0 || state.TrotterWalkFrame > 15 {
		return fmt.Errorf("game save Trotter walk frame %d is outside 0..15", state.TrotterWalkFrame)
	}
	if state.TrotterWalk != nil {
		if err := state.TrotterWalk.Validate(); err != nil {
			return fmt.Errorf("game save Trotter walk: %w", err)
		}
	}
	return nil
}

func EncodeGameProgress(state GameProgress) ([]byte, error) {
	if err := ValidateGameProgress(state); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(state, "", "\t")
	if err != nil {
		return nil, fmt.Errorf("encode game progress: %w", err)
	}
	return append(data, '\n'), nil
}

func DecodeGameProgress(data []byte) (GameProgress, error) {
	var state GameProgress
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return GameProgress{}, fmt.Errorf("decode game progress: %w", err)
	}
	if err := ValidateGameProgress(state); err != nil {
		return GameProgress{}, err
	}
	return state, nil
}

func SaveGameProgress(path string, state GameProgress) error {
	data, err := EncodeGameProgress(state)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create save directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".redust-save-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary save: %w", err)
	}
	temporaryPath := temporary.Name()
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("write temporary save: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("sync temporary save: %w", err)
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("close temporary save: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return fmt.Errorf("replace game save: %w", err)
	}
	return nil
}

func LoadGameProgress(path string) (GameProgress, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GameProgress{}, fmt.Errorf("read game save: %w", err)
	}
	return DecodeGameProgress(data)
}
