package main

import (
	"image"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"redust/engine"
	"redust/render"
	"redust/save"
)

// modalDialog is the in-game stand-in for the Win32 boxes the native engine
// raises: the Save and Open common dialogs (FUN_0042DB70 / FUN_0042DCF0, with
// the "*.rtd" type of the table at 0x0045DDB0), the MessageBoxA notice of
// notedialog (FUN_0042DFB0, MB_OK) and the Yes/No question of questiondialog
// (FUN_0042E030, MB_YESNO). While one is open the game loop is frozen, as the
// native task-modal boxes freeze it.
type modalDialog struct {
	active  bool
	base    render.IndexedFrame
	view    render.DialogView
	geom    render.DialogGeometry
	dirty   bool
	pressed int
	// buttons maps button indexes to their meaning for onClose.
	chooser    bool
	saving     bool
	dir        string
	files      []string
	name       string
	lastRow    int
	lastClick  time.Time
	blinkStart time.Time
	// onButton is called with the pressed button's index and, for a chooser,
	// the typed file name.
	onButton func(button int, name string) (render.IndexedFrame, bool, error)
}

// Active reports whether a dialog is open.
func (m *modalDialog) Active() bool { return m != nil && m.active }

func (m *modalDialog) close() { m.active = false }

func (m *modalDialog) draw() (render.IndexedFrame, error) {
	m.view.Pressed = m.pressed
	m.view.Caret = m.chooser && (time.Since(m.blinkStart)/(500*time.Millisecond))%2 == 0
	if m.chooser {
		m.view.Input = m.name
	}
	frame, geometry, err := render.DrawDialog(m.base, m.view)
	m.geom = geometry
	m.dirty = false
	return frame, err
}

// open shows view over base and returns the first frame.
func (m *modalDialog) open(base render.IndexedFrame, view render.DialogView, onButton func(int, string) (render.IndexedFrame, bool, error)) (render.IndexedFrame, bool, error) {
	m.active, m.base, m.view, m.onButton, m.pressed, m.blinkStart = true, base, view, onButton, -1, time.Now()
	m.chooser = false
	frame, err := m.draw()
	return frame, true, err
}

// Notice is notedialog: one OK button (MB_OK | MB_ICONASTERISK, 0x2040).
func (m *modalDialog) Notice(base render.IndexedFrame, text string, then func() (render.IndexedFrame, bool, error)) (render.IndexedFrame, bool, error) {
	lines, err := render.WrapDialogText(text, 296)
	if err != nil {
		return render.IndexedFrame{}, false, err
	}
	return m.open(base, render.DialogView{Title: "Dust", Lines: lines, Buttons: []string{"OK"}, Pressed: -1}, func(int, string) (render.IndexedFrame, bool, error) {
		m.close()
		if then == nil {
			return base, true, nil
		}
		return then()
	})
}

// Question is questiondialog: Yes and No (MB_YESNO | MB_ICONQUESTION,
// 0x2024); only IDYES is true.
func (m *modalDialog) Question(base render.IndexedFrame, text string, then func(yes bool) (render.IndexedFrame, bool, error)) (render.IndexedFrame, bool, error) {
	lines, err := render.WrapDialogText(text, 296)
	if err != nil {
		return render.IndexedFrame{}, false, err
	}
	return m.open(base, render.DialogView{Title: "Dust", Lines: lines, Buttons: []string{"Yes", "No"}, Pressed: -1}, func(button int, _ string) (render.IndexedFrame, bool, error) {
		m.close()
		return then(button == 0)
	})
}

// savesDirectory is where native-format saves live, under the work directory.
func savesDirectory(workDir string) string { return filepath.Join(workDir, "saves") }

// saveFileNames lists the *.rtd files in dir without their extension, sorted
// case-insensitively.
func saveFileNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), "."+save.NativeFileTypeRTD) {
			continue
		}
		names = append(names, strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names
}

// cleanSaveName strips an extension the player typed and rejects names the
// file system cannot hold.
func cleanSaveName(text string) string {
	text = strings.TrimSpace(text)
	if strings.EqualFold(filepath.Ext(text), "."+save.NativeFileTypeRTD) {
		text = strings.TrimSpace(text[:len(text)-len(filepath.Ext(text))])
	}
	if text == "" || text == "." || text == ".." || strings.ContainsAny(text, "<>:\"/\\|?*") {
		return ""
	}
	return text
}

const chooserRows = 7

// Choose opens the Save As or Open dialog. then receives the chosen file name
// (without extension) or ok=false when the player cancelled; the file is not
// touched. initial is the name the edit box starts with.
func (m *modalDialog) Choose(base render.IndexedFrame, workDir string, saving bool, initial string, then func(name string, ok bool) (render.IndexedFrame, bool, error)) (render.IndexedFrame, bool, error) {
	title, accept := "Open", "Open"
	if saving {
		title, accept = "Save As", "Save"
	}
	m.dir = savesDirectory(workDir)
	m.files = saveFileNames(m.dir)
	m.saving, m.name, m.lastRow = saving, initial, -1
	view := render.DialogView{Title: title, ShowInput: true, InputLabel: "File name:", ShowList: true, ListLabel: "Files in saves:", ListRows: chooserRows, ListSelected: -1, FilterNote: "Files of type: Saved games (." + strings.ToUpper(save.NativeFileTypeRTD) + ")", Buttons: []string{accept, "Cancel"}, Pressed: -1}
	view.List = m.files
	frame, changed, err := m.open(base, view, nil)
	m.chooser = true
	m.syncSelection()
	m.onButton = func(button int, _ string) (render.IndexedFrame, bool, error) {
		if button != 0 {
			m.close()
			return then("", false)
		}
		name := cleanSaveName(m.name)
		if name == "" {
			return m.Notice(m.base, "The file name is not valid.\nType a name for the saved game.", func() (render.IndexedFrame, bool, error) {
				return m.Choose(base, workDir, saving, m.name, then)
			})
		}
		exists := false
		for _, existing := range m.files {
			if strings.EqualFold(existing, name) {
				exists, name = true, existing
			}
		}
		keep := m.name
		if saving && exists {
			return m.Question(m.base, name+"."+save.NativeFileTypeRTD+" already exists.\nDo you want to replace it?", func(yes bool) (render.IndexedFrame, bool, error) {
				if yes {
					return then(name, true)
				}
				return m.Choose(base, workDir, saving, keep, then)
			})
		}
		if !saving && !exists {
			return m.Notice(m.base, name+"."+save.NativeFileTypeRTD+"\nFile not found.\nPlease verify the correct file name was given.", func() (render.IndexedFrame, bool, error) {
				return m.Choose(base, workDir, saving, keep, then)
			})
		}
		m.close()
		return then(name, true)
	}
	if err == nil {
		frame, err = m.draw()
	}
	return frame, changed, err
}

// syncSelection highlights the list row matching the typed name.
func (m *modalDialog) syncSelection() {
	m.view.ListSelected = -1
	for index, name := range m.files {
		if strings.EqualFold(name, strings.TrimSpace(m.name)) {
			m.view.ListSelected = index
			if index < m.view.ListTop {
				m.view.ListTop = index
			} else if index >= m.view.ListTop+chooserRows {
				m.view.ListTop = index - chooserRows + 1
			}
		}
	}
	m.dirty = true
}

// HandleText appends typed characters to the edit box.
func (m *modalDialog) HandleText(text string) {
	if !m.Active() || !m.chooser {
		return
	}
	for _, r := range text {
		if r < 32 || r == 127 || strings.ContainsRune("<>:\"/\\|?*", r) || len(m.name) >= 56 {
			continue
		}
		m.name += string(r)
	}
	m.syncSelection()
}

func (m *modalDialog) press(button int) (render.IndexedFrame, bool, error) {
	if m.onButton == nil || button < 0 {
		return m.base, false, nil
	}
	return m.onButton(button, m.name)
}

// Update redraws the box when its state changed or the caret blinked.
func (m *modalDialog) Update() (render.IndexedFrame, bool, error) {
	if !m.Active() {
		return render.IndexedFrame{}, false, nil
	}
	caret := m.chooser && (time.Since(m.blinkStart)/(500*time.Millisecond))%2 == 0
	if !m.dirty && caret == m.view.Caret {
		return render.IndexedFrame{}, false, nil
	}
	frame, err := m.draw()
	return frame, err == nil, err
}

// HandleKey routes a key press to the open dialog.
func (m *modalDialog) HandleKey(key ebiten.Key) (render.IndexedFrame, bool, error) {
	switch key {
	case ebiten.KeyEnter, ebiten.KeyNumpadEnter:
		return m.press(0)
	case ebiten.KeyEscape:
		if len(m.view.Buttons) > 1 {
			return m.press(len(m.view.Buttons) - 1)
		}
		return m.press(0)
	case ebiten.KeyY:
		if !m.chooser && len(m.view.Buttons) == 2 && m.view.Buttons[0] == "Yes" {
			return m.press(0)
		}
	case ebiten.KeyN:
		if !m.chooser && len(m.view.Buttons) == 2 && m.view.Buttons[0] == "Yes" {
			return m.press(1)
		}
	case ebiten.KeyBackspace:
		if m.chooser && m.name != "" {
			m.name = m.name[:len(m.name)-1]
			m.syncSelection()
		}
	case ebiten.KeyDelete:
		if m.chooser {
			m.name = ""
			m.syncSelection()
		}
	case ebiten.KeyArrowUp, ebiten.KeyArrowDown, ebiten.KeyPageUp, ebiten.KeyPageDown:
		if m.chooser && len(m.files) > 0 {
			step := map[ebiten.Key]int{ebiten.KeyArrowUp: -1, ebiten.KeyArrowDown: 1, ebiten.KeyPageUp: -chooserRows, ebiten.KeyPageDown: chooserRows}[key]
			next := m.view.ListSelected + step
			if m.view.ListSelected < 0 {
				next = 0
			}
			next = max(0, min(len(m.files)-1, next))
			m.name = m.files[next]
			m.syncSelection()
		}
	}
	return m.Update()
}

func pointOf(point uint32) image.Point { return image.Pt(int(int16(point>>16)), int(int16(point))) }

// HandleMouseDown handles a left click on the dialog.
func (m *modalDialog) HandleMouseDown(point uint32) (render.IndexedFrame, bool, error) {
	at := pointOf(point)
	for index, rect := range m.geom.Buttons {
		if at.In(rect) {
			m.pressed = index
			m.dirty = true
			return m.Update()
		}
	}
	if m.chooser {
		for row, rect := range m.geom.Rows {
			entry := m.view.ListTop + row
			if at.In(rect) && entry < len(m.files) {
				double := m.lastRow == entry && time.Since(m.lastClick) < 450*time.Millisecond
				m.name, m.lastRow, m.lastClick = m.files[entry], entry, time.Now()
				m.syncSelection()
				if double {
					return m.press(0)
				}
				return m.Update()
			}
		}
		if at.In(m.geom.ScrollUp) && m.view.ListTop > 0 {
			m.view.ListTop--
			m.dirty = true
		}
		if at.In(m.geom.ScrollDown) && m.view.ListTop+chooserRows < len(m.files) {
			m.view.ListTop++
			m.dirty = true
		}
	}
	return m.Update()
}

// HandleMouseState finishes a button press on release over the same button.
func (m *modalDialog) HandleMouseState(state engine.MouseState) (render.IndexedFrame, bool, error) {
	if m.pressed < 0 {
		return render.IndexedFrame{}, false, nil
	}
	if state.LeftDown && !state.LeftReleased {
		return render.IndexedFrame{}, false, nil
	}
	button := m.pressed
	inside := pointOf(state.Point).In(m.geom.Buttons[button])
	m.pressed, m.dirty = -1, true
	if !inside {
		return m.Update()
	}
	return m.press(button)
}
