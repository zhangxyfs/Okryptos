//go:build windows

package main

import (
	"testing"

	"okryptos/internal/gui"
)

func TestPlacementFromStateMaximized(t *testing.T) {
	wp := placementFromState(&gui.WindowState{Maximized: true, Left: 1, Top: 2, Right: 801, Bottom: 601})
	if wp.ShowCmd != swMaximize {
		t.Fatalf("ShowCmd got %d, want %d", wp.ShowCmd, swMaximize)
	}
	if wp.RcNormalPosition != [4]int32{1, 2, 801, 601} {
		t.Fatalf("rect got %v", wp.RcNormalPosition)
	}
}

func TestStateFromPlacement(t *testing.T) {
	wp := windowPlacement{ShowCmd: swShowNormal, RcNormalPosition: [4]int32{5, 6, 805, 606}}
	s := stateFromPlacement(&wp)
	if s.Maximized || s.Left != 5 || s.Bottom != 606 {
		t.Fatalf("got %+v", s)
	}
}
