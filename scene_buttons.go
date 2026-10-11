package main

import (
	"redust/assets"
	"redust/scripts"
)

// flatButtons is the button block of the named flat in the open stage: the rows
// pointinbutton searches. FUN_00413590 finds the flat by name (first match, ASCII
// case folded), and the flat's record dword at +8 names the button block, which
// is the scene's SceneHandlers resource (Fields[2]). found is false when the
// stage has no flat of that name.
func flatButtons(stage *assets.Stage, flat string) ([]scripts.SceneButton, bool, error) {
	if stage == nil {
		return nil, false, nil
	}
	for index, scene := range stage.Scenes {
		if !scripts.EqualASCIIFold(scripts.PascalTextOf(scene.Name), flat) {
			continue
		}
		handlers, err := stage.SceneHandlers(index)
		if err != nil {
			return nil, true, err
		}
		buttons := make([]scripts.SceneButton, len(handlers))
		for i, handler := range handlers {
			buttons[i] = scripts.SceneButton{
				Name: scripts.PascalTextOf(handler.Name),
				Rect: [4]int16{handler.Top, handler.Left, handler.Bottom, handler.Right},
			}
		}
		return buttons, true, nil
	}
	return nil, false, nil
}
