package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

type Flame struct {
	width   int
	height  int
	grid    []int8
	buffer  *bytes.Buffer
	renders int
	rand    *rand.Rand
}

type Dimensions struct {
	Width  int
	Height int
}

func mapColor(v int8) [3]uint8 {
	cmap := [][3]uint8{
		{0x07, 0x07, 0x07}, {0x1f, 0x07, 0x07}, {0x2f, 0x0f, 0x07},
		{0x47, 0x0f, 0x07}, {0x57, 0x17, 0x07}, {0x67, 0x1f, 0x07},
		{0x77, 0x1f, 0x07}, {0x8f, 0x27, 0x07}, {0x9f, 0x2f, 0x07},
		{0xaf, 0x3f, 0x07}, {0xbf, 0x47, 0x07}, {0xc7, 0x47, 0x07},
		{0xdf, 0x4f, 0x07}, {0xdf, 0x57, 0x07}, {0xdf, 0x57, 0x07},
		{0xd7, 0x5f, 0x07}, {0xd7, 0x67, 0x0f}, {0xcf, 0x6f, 0x0f},
		{0xcf, 0x77, 0x0f}, {0xcf, 0x7f, 0x0f}, {0xcf, 0x87, 0x17},
		{0xc7, 0x87, 0x17}, {0xc7, 0x8f, 0x17}, {0xc7, 0x97, 0x1f},
		{0xbf, 0x9f, 0x1f}, {0xbf, 0x9f, 0x1f}, {0xbf, 0xa7, 0x27},
		{0xbf, 0xa7, 0x27}, {0xbf, 0xaf, 0x2f}, {0xb7, 0xaf, 0x2f},
		{0xb7, 0xb7, 0x2f}, {0xb7, 0xb7, 0x37}, {0xcf, 0xcf, 0x6f},
		{0xdf, 0xdf, 0x9f}, {0xef, 0xef, 0xc7}, {0xff, 0xff, 0xff},
	}

	if v < 0 || int(v) >= len(cmap) {
		return [3]uint8{0, 0, 0}
	}

	return cmap[v]
}

func (flame *Flame) SetDimensions(d Dimensions) {
	flame.width = d.Width
	flame.height = d.Height
	flame.Init()
}

func (flame *Flame) Init() {
	// initialize our fire grid

	// the bottom most row is the source of our "flame" and is set to the higest
	// possible value on the grid.
	//
	// this value is "spread" upward.
	flame.grid = make([]int8, flame.width*flame.height)
	for j := 0; j < flame.width; j++ {
		flame.grid[((flame.height-1)*flame.width)+j] = 35
	}
}

func (flame *Flame) Spread() {
	for y := flame.height - 1; y > 0; y-- {
		for x := 0; x < flame.width; x++ {
			src := (y * flame.width) + x

			// generate random number between [0, 6) and and subtract 3 from it.
			// this biases the results to < 0 which shifts the direction of the
			// flames to the left giving a wind effect.
			dst := (src - flame.width) + flame.rand.Intn(6) - 2

			// if destination is outside of the bounds of our display, skip it.
			if start, end := (y-1)*flame.width, y*flame.width+flame.width; dst < start || dst > end {
				continue
			}

			if end := (flame.width * flame.height) - 1; dst > end {
				dst = end
			}

			// sometimes the flames get a little more intense as they rise.
			flame.grid[dst] = flame.grid[src] - int8(flame.rand.Intn(6)-1)

			// clip grid values to within our range.
			if flame.grid[dst] > 35 {
				flame.grid[dst] = 35
			}

			if flame.grid[dst] < 0 {
				flame.grid[dst] = 0
			}
		}
	}
}

func (flame *Flame) Render() {
	flame.buffer.WriteString("\x1b[0;0H")

	prevbg, prevfg := [3]uint8{}, [3]uint8{}
	for y := 0; y < flame.height; y += 2 {
		for x := 0; x < flame.width; x++ {
			if c := mapColor(flame.grid[(y*flame.width)+x]); c != prevfg {
				// change foreground color
				flame.buffer.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c[0], c[1], c[2]))
				prevfg = c
			}

			if c := mapColor(flame.grid[((y+1)*flame.width)+x]); c != prevbg {
				// change background color
				flame.buffer.WriteString(fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c[0], c[1], c[2]))
				prevbg = c
			}
			flame.buffer.WriteString("▀")
		}
	}

	flame.renders++

	io.Copy(os.Stdout, flame.buffer)
	flame.buffer.Reset()
	time.Sleep(100 * time.Millisecond)
}

func withDimentions(width int, height int) func(*Flame) error {
	return func(i *Flame) error {
		i.width = width
		i.height = height
		return nil
	}
}

func newFlame(opts ...func(*Flame) error) (*Flame, error) {
	rc := Flame{}

	rc.buffer = &bytes.Buffer{}
	rc.rand = rand.New(rand.NewSource(time.Now().UnixNano()))

	for _, opt := range opts {
		if err := opt(&rc); err != nil {
			return nil, err
		}
	}
	return &rc, nil
}

func fire(ctx context.Context) chan Dimensions {
	rc := make(chan Dimensions)
	go func() {
		flame, err := newFlame(withDimentions(0, 0))

		if err != nil {
			return
		}

		for {
			select {
			case <-ctx.Done():
				rc <- Dimensions{}
				return

			case dims := <-rc:
				flame.SetDimensions(dims)

			default:
				// display grid
				flame.Render()

				// percollate values up
				flame.Spread()
			}
		}
	}()
	return rc
}

func main() {
	width, height, err := term.GetSize(0)

	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	dims := fire(ctx)
	dims <- Dimensions{Width: width, Height: height * 2}

	sigs := make(chan os.Signal)
	signal.Notify(sigs, syscall.SIGWINCH, syscall.SIGINT)

innerloop:
	for sig := range sigs {
		switch sig {
		case syscall.SIGWINCH:
			width, height, _ := term.GetSize(0)
			dims <- Dimensions{Width: width, Height: height * 2}
		case syscall.SIGINT:
			cancel()
			break innerloop
		}
	}

	<-dims

	os.Stdout.Write([]byte("\x1b[39;m"))
	os.Stdout.Write([]byte("\x1b[49;m"))
}
