package stats

import (
	"fmt"

	"github.com/rivo/tview"
)

type StatsBackend interface {
	RegisterStat(name string)
	UpdateStat(name string, value string) error
	Draw()
}

type StatsView struct {
	stats      []string
	statValues map[string]string
	table      *tview.Table
	app        *tview.Application
}

// NewStatsView returns a new Statistics Viewer
func NewStatsView(app *tview.Application) *StatsView {
	table := tview.NewTable()
	table.SetTitle("Statistics").SetBorder(true).SetBorderPadding(1, 1, 2, 0).SetTitle("Stats")
	return &StatsView{
		app:        app,
		stats:      make([]string, 0),
		statValues: make(map[string]string, 0),
		table:      table,
	}
}

// View returns the main primitive
func (s *StatsView) View() tview.Primitive {
	return s.table
}

// RegisterStat registers a new statistic
func (s *StatsView) RegisterStat(name string) {
	s.stats = append(s.stats, name)
}

// UpdateStat updates a statistic value
// Caller must call Draw() after all stats have been updated
func (s *StatsView) UpdateStat(name string, value string) error {
	for _, stat := range s.stats {
		if stat == name {
			s.statValues[name] = value
		}
	}
	return fmt.Errorf("Statistic not registered %s", name)
}

func (s *StatsView) Draw() {
	go s.app.QueueUpdateDraw(s.refresh)
}

func (s *StatsView) refresh() {
	for i, stat := range s.stats {
		s.table.SetCell(i, 0, tview.NewTableCell(fmt.Sprintf("%s: ", stat)))
		s.table.SetCell(i, 1, tview.NewTableCell(s.statValues[stat]))
	}
}
