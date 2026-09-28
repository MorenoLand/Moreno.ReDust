package render

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"redust/assets"
)

type playerInventorySlot struct {
	name     string
	propName string
	handName string
	anchor   image.Point
}

var playerInventorySlotsByDay = map[int][]playerInventorySlot{
	1: {
		{name: "bone", propName: "Bone", handName: "bone", anchor: image.Pt(185, 252)},
		{name: "ring", propName: "Ring", handName: "ring", anchor: image.Pt(95, 221)},
		{name: "cigar", propName: "Cigar", handName: "cigar", anchor: image.Pt(158, 124)},
		{name: "hrkey", propName: "HRKey", handName: "hrkey", anchor: image.Pt(75, 141)},
		{name: "cards", propName: "Cards", handName: "cards", anchor: image.Pt(347, 219)},
		{name: "bknife", propName: "BKnife", handName: "bknife", anchor: image.Pt(358, 128)},
		{name: "mask", propName: "Mask", handName: "mask", anchor: image.Pt(179, 178)},
		{name: "hhkey", propName: "HHKEY", handName: "hhkey", anchor: image.Pt(386, 273)},
	},
	2: {
		{name: "biscuits", propName: "Biscuits", handName: "biscuits", anchor: image.Pt(249, 304)},
		{name: "ring", propName: "Ring", handName: "ring", anchor: image.Pt(416, 204)},
		{name: "cigar", propName: "Cigar", handName: "cigar", anchor: image.Pt(431, 100)},
		{name: "hrkey", propName: "HRKey", handName: "hrkey", anchor: image.Pt(444, 270)},
		{name: "cards", propName: "Cards", handName: "cards", anchor: image.Pt(188, 105)},
		{name: "bknife", propName: "BKnife", handName: "bknife", anchor: image.Pt(70, 251)},
		{name: "mask", propName: "Mask", handName: "mask", anchor: image.Pt(387, 266)},
		{name: "hhkey", propName: "HHKEY", handName: "hhkey", anchor: image.Pt(420, 320)},
		{name: "jug", propName: "Jug", handName: "jug", anchor: image.Pt(326, 275)},
		{name: "flowers", propName: "Flowers", handName: "flowers", anchor: image.Pt(450, 146)},
		{name: "sugarcubes", propName: "Sugarcubes", handName: "sugarcubes", anchor: image.Pt(92, 152)},
		{name: "pie", propName: "Pie", handName: "pie", anchor: image.Pt(75, 188)},
		{name: "hankerchief", propName: "Hankerchief", handName: "hankerchief", anchor: image.Pt(95, 320)},
		{name: "bullets", propName: "Bullets", handName: "bullets", anchor: image.Pt(271, 151)},
		{name: "harmonica", propName: "Harmonica", handName: "harmonica", anchor: image.Pt(181, 153)},
		{name: "history", propName: "History", handName: "history", anchor: image.Pt(345, 190)},
		{name: "postcards", propName: "Postcards", handName: "postcards", anchor: image.Pt(130, 264)},
		{name: "yunnibook", propName: "Yunnibook", handName: "yunnibook", anchor: image.Pt(192, 128)},
		{name: "hairpin", propName: "HAIRPIN", handName: "hairpin", anchor: image.Pt(97, 108)},
		{name: "balm", propName: "Balm", handName: "balm", anchor: image.Pt(166, 205)},
		{name: "apple", propName: "Apple", handName: "apple", anchor: image.Pt(292, 228)},
		{name: "boots", propName: "Boots", handName: "boots", anchor: image.Pt(344, 110)},
		{name: "badge", propName: "Badge", handName: "badge", anchor: image.Pt(395, 139)},
	},
	3: {
		{name: "biscuits", propName: "Biscuits", handName: "biscuits", anchor: image.Pt(249, 304)},
		{name: "cigar", propName: "Cigar", handName: "cigar", anchor: image.Pt(344, 221)},
		{name: "tbird", propName: "Tbird", handName: "tbird", anchor: image.Pt(425, 230)},
		{name: "mask", propName: "Mask", handName: "mask", anchor: image.Pt(390, 287)},
		{name: "jug", propName: "Jug", handName: "jug", anchor: image.Pt(326, 275)},
		{name: "pages", propName: "Pages", handName: "pages", anchor: image.Pt(136, 263)},
		{name: "matchbox", propName: "Matchbox", handName: "matchbox", anchor: image.Pt(450, 117)},
		{name: "flute", propName: "Flute", handName: "flute", anchor: image.Pt(198, 242)},
		{name: "hairpin", propName: "HAIRPIN", handName: "hairpin", anchor: image.Pt(95, 320)},
		{name: "bullets", propName: "Bullets", handName: "bullets", anchor: image.Pt(271, 151)},
		{name: "harmonica", propName: "Harmonica", handName: "harmonica", anchor: image.Pt(385, 127)},
		{name: "seed", propName: "Seed", handName: "seed", anchor: image.Pt(444, 289)},
		{name: "yunnibook", propName: "Yunnibook", handName: "yunnibook", anchor: image.Pt(191, 140)},
		{name: "rx", propName: "RX", handName: "rx", anchor: image.Pt(196, 245)},
		{name: "sugarcubes", propName: "Sugarcubes", handName: "sugarcubes", anchor: image.Pt(95, 150)},
		{name: "apple", propName: "Apple", handName: "apple", anchor: image.Pt(433, 180)},
		{name: "boots", propName: "Boots", handName: "boots", anchor: image.Pt(358, 182)},
		{name: "badge", propName: "Badge", handName: "badge", anchor: image.Pt(150, 108)},
	},
	4: {
		{name: "biscuits", propName: "Biscuits", handName: "biscuits", anchor: image.Pt(249, 304)},
		{name: "cigar", propName: "Cigar", handName: "cigar", anchor: image.Pt(344, 221)},
		{name: "tbird", propName: "Tbird", handName: "tbird", anchor: image.Pt(425, 230)},
		{name: "tstone", propName: "Tstone", handName: "tstone", anchor: image.Pt(459, 160)},
		{name: "mask", propName: "Mask", handName: "mask", anchor: image.Pt(390, 287)},
		{name: "jug", propName: "Jug", handName: "jug", anchor: image.Pt(326, 275)},
		{name: "pages", propName: "Pages", handName: "pages", anchor: image.Pt(149, 275)},
		{name: "matchbox", propName: "Matchbox", handName: "matchbox", anchor: image.Pt(413, 126)},
		{name: "flute", propName: "Flute", handName: "flute", anchor: image.Pt(198, 242)},
		{name: "bullets", propName: "Bullets", handName: "bullets", anchor: image.Pt(271, 151)},
		{name: "harmonica", propName: "Harmonica", handName: "harmonica", anchor: image.Pt(339, 109)},
		{name: "seed", propName: "Seed", handName: "seed", anchor: image.Pt(444, 289)},
		{name: "yunnibook", propName: "Yunnibook", handName: "yunnibook", anchor: image.Pt(191, 140)},
		{name: "blade", propName: "Blade", handName: "blade", anchor: image.Pt(341, 58)},
		{name: "boots", propName: "Boots", handName: "boots", anchor: image.Pt(367, 176)},
	},
}

func BuildPlayerInventoryProps(archive *assets.PropArchive, owners map[string]string, hidden map[string]bool, selected string, day int) ([]FlatPropSprite, error) {
	if archive == nil {
		return nil, fmt.Errorf("player inventory has no prop archive")
	}
	slots := playerInventorySlotsByDay[day]
	props := make([]FlatPropSprite, 0, len(slots))
	for _, slot := range slots {
		if owners[slot.name] != "stranger" || hidden[slot.name] {
			continue
		}
		viewName := "PANEL"
		if strings.EqualFold(selected, slot.handName) || strings.EqualFold(selected, slot.name) {
			viewName = "HILITE"
		}
		props = append(props, FlatPropSprite{Name: slot.handName, PropName: slot.propName, ViewName: viewName, Anchor: slot.anchor, Archive: archive})
	}
	return props, nil
}

func DrawPlayerInventoryCash(frame IndexedFrame, cash int32) (IndexedFrame, error) {
	return DrawNativeTextAtColor(frame, fmt.Sprintf("$%d", cash), image.Pt(387, 74), color.RGBA{R: 164, G: 112, B: 69, A: 255})
}
