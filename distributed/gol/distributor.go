package gol

import (
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"
	"time"

	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
	keyPresses <-chan rune
}

// World type the world struct
type World struct {
	Width       int
	Height      int
	Turns       int
	Threads     int
	CurLattice  *Slice
	PrevLattice *Slice
}

// Slice is a 2D slice of cells.
type Slice struct {
	Width   int
	Height  int
	Threads int
	Cells   [][]bool
}

// LocalControl is the local control goroutine.
type LocalControl struct {
	turns       int
	currentTurn *int
	events      chan<- Event
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {
	worker, err := rpc.Dial("tcp", "3.83.23.107:8081")
	if err != nil {
		panic(err)
	}
	defer worker.Close()

	//Create a 2D slice to store the world.
	world := &World{
		Height:  p.ImageHeight,
		Width:   p.ImageWidth,
		Turns:   p.Turns,
		Threads: p.Threads,
	}
	world.CurLattice = NewSlice(p)
	world.PrevLattice = NewSlice(p)

	//load the file    world <- file
	filename := fmt.Sprintf("%dx%d", p.ImageHeight, p.ImageWidth)
	c.ioCommand <- ioInput
	c.ioFilename <- filename
	for y := 0; y < world.Height; y++ {
		for x := 0; x < world.Width; x++ {
			world.CurLattice.InitialCellState(x, y, <-c.ioInput == 255)
		}
	}

	// Execute all turns of the Game of Life.
	turn := 0

	listener, err := net.Listen("tcp", "localhost:8888")
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	rpc.Register(&LocalControl{p.Turns, &turn, c.events})
	go rpc.Accept(listener)

	world.CellFlipped(turn, c) // send the initial state to the channel

	go func(turn *int) {
		ticker := time.NewTicker(2 * time.Second)
		for *turn <= p.Turns {
			select {
			case <-ticker.C:
				if *turn >= p.Turns-1 {
					ticker.Stop()
					return
				}
				var resp stubs.AliveCellsCountResp
				err := worker.Call(stubs.AliveCellsCount, stubs.AliveCellsCountReq{}, &resp)
				if err != nil {
					panic(err)
				}
				c.events <- AliveCellsCount{
					CompletedTurns: resp.Turn,
					CellsCount:     resp.Count,
				}
			case key := <-c.keyPresses:
				var resp stubs.KeyPressResp
				err := worker.Call(stubs.KeyPress, stubs.KeyPressReq{Key: key}, &resp)
				if err != nil {
					panic(err)
				}
				switch key {
				case 'p':
					c.events <- StateChange{
						CompletedTurns: resp.Turn,
						NewState:       Paused,
					}
					for {
						key := <-c.keyPresses
						if key == 'p' {
							err := worker.Call(stubs.KeyPress, stubs.KeyPressReq{Key: key}, &resp)
							if err != nil {
								panic(err)
							}
							c.events <- StateChange{
								CompletedTurns: resp.Turn,
								NewState:       Executing,
							}
							fmt.Println("Continuing")
							break
						}
					}
				case 'q':
					c.events <- StateChange{
						CompletedTurns: resp.Turn,
						NewState:       Quitting,
					}
					close(c.events)
					os.Exit(0)
				case 's':
					world.CurLattice.Cells = resp.World.Cells
					world.GenerateFile(resp.Turn, p, c)
				case 'k':
					world.CurLattice.Cells = resp.World.Cells
					world.GenerateFile(resp.Turn, p, c)
					if key == 'k' {
						c.events <- StateChange{
							CompletedTurns: resp.Turn,
							NewState:       Quitting,
						}
						close(c.events)
						os.Exit(0)
					}
				}
			}
		}
	}(&turn)

	req := stubs.GameOfLifeReq{
		World: stubs.World{
			Width:   world.Width,
			Height:  world.Height,
			Threads: world.Threads,
			Cells:   world.CurLattice.Cells,
			Turns:   world.Turns,
		},
	}

	var resp stubs.GameOfLifeResp
	err = worker.Call(stubs.GameOfLife, req, &resp)
	if err != nil {
		panic(err)
	}

	turn = resp.Turns
	world.CurLattice.Cells = resp.World.Cells

	//Report the final state using FinalTurnCompleteEvent.
	var cells []util.Cell
	for y := 0; y < world.Height; y++ {
		for x := 0; x < world.Width; x++ {
			if world.CurLattice.CellState(x, y) {
				cells = append(cells, util.Cell{X: x, Y: y})
			}
		}
	}
	c.events <- FinalTurnComplete{ //send to channel
		CompletedTurns: world.Turns,
		Alive:          cells,
	}
	//save the file and output.
	filename = fmt.Sprintf("%dx%dx%d", p.ImageHeight, p.ImageWidth, turn)
	c.ioCommand <- ioOutput
	c.ioFilename <- filename
	for y := 0; y < world.Height; y++ {
		for x := 0; x < world.Width; x++ {
			var value uint8
			if world.CurLattice.CellState(x, y) {
				value = 255 //alive
			} else {
				value = 0 //dead
			}
			c.ioOutput <- value
		}
	}
	c.events <- ImageOutputComplete{
		Filename:       filename,
		CompletedTurns: turn,
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- StateChange{turn, Quitting}
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}

// NewSlice creates a new slice.
func NewSlice(p Params) *Slice {
	slice := &Slice{
		Width:   p.ImageWidth,
		Height:  p.ImageHeight,
		Threads: p.Threads,
	}
	slice.Cells = make([][]bool, slice.Height)
	for i := range slice.Cells {
		slice.Cells[i] = make([]bool, slice.Width)
	}
	return slice
}

// Run runs the distributor. (go by example mockup)
func (w *World) Run() {
	var wg sync.WaitGroup
	for y := 0; y < w.Height; {
		for i := 0; i < w.Threads; i++ {
			wg.Add(1)
			go func(wg *sync.WaitGroup, y int) {
				defer wg.Done()
				for x := 0; x < w.Width; x++ {
					w.PrevLattice.InitialCellState(x, y, w.CurLattice.NextStep(x, y))
				}
			}(&wg, y)
			y++
			if y == w.Height {
				break
			}
		}
	}
	wg.Wait()
	w.PrevLattice, w.CurLattice = w.CurLattice, w.PrevLattice
}

// CellFlipped send the flipped cell to the channel.
func (w *World) CellFlipped(turn int, c distributorChannels) {
	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.CurLattice.CellState(x, y) != w.PrevLattice.CellState(x, y) { //cell position changed
				c.events <- CellFlipped{
					CompletedTurns: turn, Cell: util.Cell{
						X: x,
						Y: y,
					},
				}
			}
		}
	}
}

func (s *Slice) InitialCellState(x, y int, alive bool) {
	if x >= 0 && x < s.Width && y >= 0 && y < s.Height {
		s.Cells[x][y] = alive
	}
}

// CellState returns the position of the cell .
func (s *Slice) CellState(x, y int) bool {
	if x >= 0 && x < s.Width && y >= 0 && y < s.Height {
		return s.Cells[x][y]
	} else {
		if x == s.Width || x == (-1) {
			x = (x + s.Width) % s.Width //closed domain
		}
		if y == s.Height || y == (-1) {
			y = (y + s.Height) % s.Height //closed domain
		}
		return s.Cells[x][y]
	}
}

func (s *Slice) NextStep(x, y int) bool {
	alive := 0
	for row := -1; row <= 1; row++ {
		for column := -1; column <= 1; column++ {
			if s.CellState(x+row, y+column) && (row != 0 || column != 0) {
				alive++
			}
		}
	}
	return alive == 3 || (s.CellState(x, y) && alive == 2)
}

func (w *World) GenerateFile(turn int, p Params, c distributorChannels) {
	filename := fmt.Sprintf("%dx%dx%d-%d", p.ImageHeight, p.ImageWidth, p.Threads, turn)
	c.events <- ImageOutputComplete{ //generate file
		Filename:       filename,
		CompletedTurns: turn,
	}
}

// GetAliveCellsCount get the alive cells number.
func (w *World) GetAliveCellsCount() int {
	var count = 0
	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.CurLattice.CellState(x, y) {
				count++
			}
		}
	}

	return count
}

// GetCellsChanged get the cells that are changed.
func (w *World) GetCellsChanged() []util.Cell {
	var cells []util.Cell
	for y := 0; y < w.Height; y++ {
		for x := 0; x < w.Width; x++ {
			if w.CurLattice.CellState(x, y) != w.PrevLattice.CellState(x, y) {
				cells = append(cells, util.Cell{X: x, Y: y})
			}
		}
	}
	return cells
}

// TurnComplete send the turn complete to channel.
func (l *LocalControl) TurnComplete(req stubs.TurnCompleteReq, resp *stubs.TurnCompleteResp) error {
	if req.Turn >= l.turns {
		return nil
	}

	*l.currentTurn = req.Turn
	for i := 0; i < len(req.Cells); i++ {
		l.events <- CellFlipped{
			CompletedTurns: req.Turn,
			Cell:           req.Cells[i],
		}
	}

	l.events <- TurnComplete{
		CompletedTurns: req.Turn,
	}

	return nil
}
