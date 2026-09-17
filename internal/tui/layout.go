// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Mateusz Okulanis <FPGArtktic@outlook.com>

package tui

import (
	"charm.land/bubbles/v2/table"
	"github.com/charmbracelet/x/ansi"

	"github.com/FPGArtktic/lazysubmodules/internal/core"
	"github.com/FPGArtktic/lazysubmodules/internal/manifest"
)

// Screen geometry.
const (
	// previewMinScreen is the narrowest screen that shows the preview.
	previewMinScreen = 80
	// previewMinWidth is the narrowest preview panel.
	previewMinWidth = 24
	// minScreenWidth and minScreenHeight are the smallest usable screen.
	minScreenWidth  = 20
	minScreenHeight = 6
	// barLines is the number of lines below the frame: status and keys.
	barLines = 2
	// maxNameWidth and maxRefWidth limit the natural column widths.
	maxNameWidth = 32
	maxRefWidth  = 24
	// minFlexWidth is the space for the name and ref columns below which
	// the lock column, and then the mode column, are dropped.
	minFlexWidth = 12
)

// Column titles of the submodule table.
const (
	titleName  = "NAME"
	titleMode  = "MODE"
	titleRef   = "REF"
	titleLock  = "LOCK"
	titleState = "STATE"
)

// placeholder fills a cell that has no value.
const placeholder = "-"

// widths holds the widths of the table columns; a width of zero hides
// the column.
type widths struct {
	name, mode, ref, lock, state int
}

// columns returns the table columns with the given widths.
func columns(w widths) []table.Column {
	return []table.Column{
		{Title: titleName, Width: w.name},
		{Title: titleMode, Width: w.mode},
		{Title: titleRef, Width: w.ref},
		{Title: titleLock, Width: w.lock},
		{Title: titleState, Width: w.state},
	}
}

// total returns the width of a table row: every shown column has one cell
// of padding on its left.
func (w widths) total() int {
	sum := 0
	for _, c := range []int{w.name, w.mode, w.ref, w.lock, w.state} {
		if c > 0 {
			sum += c + 1
		}
	}
	return sum
}

// cells returns the plain cell values of a submodule.
func cells(st core.Status) [5]string {
	sub := st.Submodule
	mode, ref, lk := placeholder, placeholder, placeholder
	if sub.Managed() {
		mode, ref = string(sub.Mode), clean(sub.Ref)
		if sub.Mode == manifest.ModeCommit {
			ref = shortCommit(ref)
		}
	}
	if st.Lock != nil {
		lk = shortCommit(st.Lock.Commit)
	}
	return [5]string{clean(sub.Name), mode, ref, lk, string(st.State)}
}

// naturalWidths returns the column widths that show every value of the
// table, within limits for names and refs.
func naturalWidths(statuses []core.Status) widths {
	w := widths{
		name: len(titleName), mode: len(titleMode), ref: len(titleRef),
		lock: len(titleLock), state: len(titleState),
	}
	for _, st := range statuses {
		c := cells(st)
		w.name = max(w.name, ansi.StringWidth(c[0]))
		w.mode = max(w.mode, len(c[1]))
		w.ref = max(w.ref, ansi.StringWidth(c[2]))
		w.lock = max(w.lock, len(c[3]))
		w.state = max(w.state, len(c[4]))
	}
	w.name = min(w.name, maxNameWidth)
	w.ref = min(w.ref, maxRefWidth)
	return w
}

// fitWidths shrinks natural widths to the available width. The name and
// ref columns shrink first, in proportion to their natural widths; when
// they would get too little space, the lock column and then the mode
// column are hidden.
func fitWidths(natural widths, avail int) widths {
	if natural.total() <= avail {
		return natural
	}
	w := natural
	fixed := func() int { return w.total() - w.name - w.ref }
	if avail-fixed() < minFlexWidth {
		w.lock = 0
	}
	if avail-fixed() < minFlexWidth {
		w.mode = 0
	}
	flex := avail - fixed()
	w.name = max(1, flex*natural.name/(natural.name+natural.ref))
	w.ref = max(1, flex-w.name)
	return w
}

// frameLayout is the geometry of the frame around the panels.
type frameLayout struct {
	// body is the height of the frame, borders included.
	body int
	// left and right are the inner widths of the panels; right is zero
	// when the preview is hidden.
	left, right int
}

// geometry computes the frame layout for the current screen and table.
func (m Model) geometry() frameLayout {
	body := max(m.height-barLines, 0)
	if m.width < previewMinScreen {
		return frameLayout{body: body, left: max(m.width-2, 0)}
	}
	inner := m.width - 3
	left := naturalWidths(m.statuses).total()
	left = max(left, inner*2/5)
	left = min(left, inner-previewMinWidth)
	return frameLayout{body: body, left: left, right: inner - left}
}

// layout sizes the components for the current screen.
func (m *Model) layout() {
	l := m.geometry()
	m.table.SetColumns(columns(fitWidths(naturalWidths(m.statuses), l.left)))
	m.table.SetWidth(l.left)
	m.table.SetHeight(max(l.body-2, 2))
	m.keepSelectionVisible()
	vw, vh := m.viewerSize()
	m.viewer.SetWidth(vw)
	m.viewer.SetHeight(vh)
	m.sizeInput()
	if m.overlay.kind == overlayPicker {
		m.clampPicker()
	}
	m.refreshViewer()
}

// viewerSize returns the size of the text in the viewer dialog.
func (m Model) viewerSize() (int, int) {
	return max(m.width-4-4, 1), max(m.geometry().body-2, 1)
}

// dialogWidth returns the width of the small dialogs, borders included.
func (m Model) dialogWidth() int {
	const widest = 64
	return max(min(m.width-4, widest), minScreenWidth-4)
}

// keepSelectionVisible moves the table cursor to its own row again. The
// table scrolls only when the cursor is moved step by step, not when it is
// set, so the move starts at the top.
func (m *Model) keepSelectionVisible() {
	m.selectRow(m.table.Cursor())
}

// selectRow selects a row and scrolls it into view. After the rows were
// empty, the table reports the cursor -1; the row is clamped to a valid
// one.
func (m *Model) selectRow(i int) {
	n := len(m.table.Rows())
	if n == 0 {
		return
	}
	i = min(max(i, 0), n-1)
	m.table.GotoTop()
	m.table.MoveDown(i)
	m.table.SetRows(m.rows(i))
}

// rows returns the table rows. The state of every row except the selected
// one is colored; the selected row is rendered with one style throughout.
func (m Model) rows(selected int) []table.Row {
	rows := make([]table.Row, 0, len(m.statuses))
	for i, st := range m.statuses {
		c := cells(st)
		if i != selected {
			c[4] = m.styles.state(st.State)
		}
		rows = append(rows, table.Row(c[:]))
	}
	return rows
}
