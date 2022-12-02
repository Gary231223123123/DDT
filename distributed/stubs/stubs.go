package stubs

import "uk.ac.bris.cs/gameoflife/util"

const (
	GameOfLife      = "GolWorker.GameOfLife"      // GameOfLife is the name of the GameOfLife method
	AliveCellsCount = "GolWorker.AliveCellsCount" // AliveCellsCount is the name of the AliveCellsCount method
	KeyPress        = "GolWorker.KeyPress"        // KeyPress is the name of the KeyPress method
	TurnComplete    = "LocalControl.TurnComplete" // TurnComplete is the name of the TurnComplete method
)

// World is a 2D slice of bools
type World struct {
	Width   int
	Height  int
	Turns   int
	Threads int
	Cells   [][]bool
}

// GameOfLifeReq is the request for the GameOfLife method
type GameOfLifeReq struct {
	World World
}

// GameOfLifeResp is the response for the GameOfLife method
type GameOfLifeResp struct {
	World World
	Turns int
}

// TurnCompleteReq is the request for the TurnComplete method
type TurnCompleteReq struct {
	Turn  int
	Cells []util.Cell
}

// TurnCompleteResp is the response for the TurnComplete method
type TurnCompleteResp struct {
}

// AliveCellsCountReq is the request for the AliveCellsCount method
type AliveCellsCountReq struct {
}

// AliveCellsCountResp is the response for the AliveCellsCount method
type AliveCellsCountResp struct {
	Count int
	Turn  int
}

// KeyPressesReq is the request for the KeyPresses method
type KeyPressReq struct {
	Key rune
}

// KeyPressesResp is the response for the KeyPresses method
type KeyPressResp struct {
	World World
	Turn  int
	Pause bool
}
