package main

import (
	"flag"
	"fmt"
	"net"
	"net/rpc"
	"os"
	"sync"

	"uk.ac.bris.cs/gameoflife/gol"
	"uk.ac.bris.cs/gameoflife/stubs"
)

type GolWorker struct {
	world               *gol.World
	pasused             bool
	pauseChannel        chan bool
	quitChannel         chan bool
	DistributorQuitChan chan bool
	currentTurn         int
	mutex               sync.Mutex
}

// NewGolWorker creates a new worker
func NewGolWorker() *GolWorker {
	return &GolWorker{
		world:               nil,
		pasused:             false,
		pauseChannel:        make(chan bool),
		quitChannel:         make(chan bool),
		DistributorQuitChan: make(chan bool),
		currentTurn:         0,
		mutex:               sync.Mutex{},
	}
}

// GameOfLife is a method
func (g *GolWorker) GameOfLife(req stubs.GameOfLifeReq, resp *stubs.GameOfLifeResp) error {
	p := gol.Params{
		ImageHeight: req.World.Height,
		ImageWidth:  req.World.Width,
		Turns:       req.World.Turns,
		Threads:     req.World.Threads,
	}

	g.world = &gol.World{
		Width:       req.World.Width,
		Height:      req.World.Height,
		Turns:       req.World.Turns,
		Threads:     req.World.Threads,
		CurLattice:  gol.NewSlice(p),
		PrevLattice: gol.NewSlice(p),
	}

	for i := range req.World.Cells {
		copy(g.world.CurLattice.Cells[i], req.World.Cells[i])
	}

	local, err := rpc.Dial("tcp", "localhost:8888")
	if err != nil {
		return err
	}
	defer local.Close()

	for turn := 0; turn <= g.world.Turns; turn++ {
		select {
		case <-g.pauseChannel:
			fmt.Printf("Turn %d Paused\n", turn)
			for {
				<-g.pauseChannel
				fmt.Printf("Turn %d Resumed\n", turn)
				break
			}
		case <-g.quitChannel:
			fmt.Printf("Turn %d Quit\n", turn)
			os.Exit(0)
		case <-g.DistributorQuitChan:
			g.world = nil
			g.currentTurn = 0
			g.pasused = false
			fmt.Printf("Turn %d Local Quit\n", turn)
			resp.Turns = g.currentTurn
			return nil
		default:
			g.mutex.Lock()
			g.currentTurn = turn
			g.Run()
			if turn > 0 {
				cells := g.world.GetCellsChanged()
				fmt.Printf("Turn %d Changed Cells: %d\n", turn, len(cells))
				req := stubs.TurnCompleteReq{
					Turn:  turn,
					Cells: cells,
				}

				err := local.Call(stubs.TurnComplete, req, &stubs.TurnCompleteResp{})
				if err != nil {
					panic(err)
				}
			}
			g.mutex.Unlock()
		}
	}

	resp.Turns = g.currentTurn
	resp.World.Cells = g.world.PrevLattice.Cells
	fmt.Printf("Turn %d Complete\n", g.currentTurn)

	return nil
}

// Run starts the worker
func (g *GolWorker) Run() {
	var wg sync.WaitGroup
	for y := 0; y < g.world.Height; y++ {
		for i := 0; i < g.world.Threads; i++ {
			wg.Add(1)
			go func(y int, wg *sync.WaitGroup) {
				defer wg.Done()
				for x := 0; x < g.world.Width; x++ {
					g.world.PrevLattice.InitialCellState(x, y, g.world.CurLattice.NextStep(x, y))
				}
			}(y, &wg)
		}
	}

	wg.Wait()
	g.world.PrevLattice, g.world.CurLattice = g.world.CurLattice, g.world.PrevLattice
}

// AliveCellsCount is the AliveCellsCount method
func (g *GolWorker) AliveCellsCount(req stubs.AliveCellsCountReq, resp *stubs.AliveCellsCountResp) error {
	g.mutex.Lock()
	defer g.mutex.Unlock()

	resp.Count = g.world.GetAliveCellsCount()
	resp.Turn = g.currentTurn
	return nil
}

// KeyPress is the KeyPress method
func (g *GolWorker) KeyPress(req stubs.KeyPressReq, resp *stubs.KeyPressResp) error {
	resp.Turn = g.currentTurn
	switch req.Key {
	case 'p':
		g.pauseChannel <- true
		g.pasused = !g.pasused
		resp.Pause = g.pasused
	case 'q':
		g.DistributorQuitChan <- true
	case 's':
		resp.World.Cells = g.world.CurLattice.Cells
	case 'k':
		resp.World.Cells = g.world.CurLattice.Cells
		g.quitChannel <- true
	}

	return nil
}

func main() {
	port := flag.String("port", "8081", "port listen on")
	flag.Parse()
	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		panic(err)
	}
	defer listener.Close()

	fmt.Printf("Listening on port %s\n", *port)

	rpc.Register(NewGolWorker())
	rpc.Accept(listener)
}
