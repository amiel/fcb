package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/kellydunn/go-opc"
)

type Button int

const N_LEDS = 60

const (
	ModeButtonRed   Button = 408
	ModeButtonBlue  Button = 412
	ModeButtonGreen Button = 123
)

const (
	ModeButtonRedLED   = 410
	ModeButtonBlueLED  = 414
	ModeButtonGreenLED = 120
)

type NeoPattern int

const (
	PatternStop = 0
	Pattern1    = 1
	Pattern2    = 2
)

type Color struct {
	R, G, B uint8
}

func main() {
	log.Println("OH HELLO")

	buttonPresses := make(chan Button, 100)
	fadecandyBus := make(chan NeoPattern)

	// buttonPresses <- ModeButtonBlue

	going := false
	pattern := 0

	go LEDSender("localhost:7890", fadecandyBus)

	go watchButton(ModeButtonRed, buttonPresses)
	go watchButton(ModeButtonBlue, buttonPresses)
	// go watchButton(ModeButtonGreen, buttonPresses)

	setupButtonLED(ModeButtonRedLED)
	defer unexportButton(ModeButtonRedLED)
	setupButtonLED(ModeButtonBlueLED)
	defer unexportButton(ModeButtonBlueLED)
	setupButtonLED(ModeButtonGreenLED)
	defer unexportButton(ModeButtonGreenLED)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	go func() {
		for sig := range signals {
			log.Printf("captured %v, exiting..", sig)
			// os.Exit(1)
			panic("EXITING")
		}
	}()

	for press := range buttonPresses {
		log.Printf("Got Button %v\n", press)

		if press == ModeButtonRed {
			go sparkle()
		}

		if press == ModeButtonBlue {
			if going {
				going = false
				log.Println("Main: Stop FC pattern")
				fadecandyBus <- PatternStop
			} else {
				pattern++
				going = true
				log.Printf("Main: Start FC pattern pattern: %v\n", pattern)
				switch pattern % 2 {
				case 0:
					fadecandyBus <- Pattern1
				case 1:
					fadecandyBus <- Pattern2
				}
			}
		}

		// if press == ModeButtonRed {
		// 	_, err := exec.Command("mpc", "toggle").Output()

		// 	if err != nil {
		// 		fmt.Printf("%v", err)
		// 	}
		// }
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}

func sparkle() {
	for i := 0; i < 22; i += 1 {
		setButtonValue(ModeButtonBlueLED, 1)
		setButtonValue(ModeButtonGreenLED, 0)
		setButtonValue(ModeButtonRedLED, 0)
		time.Sleep(500 * time.Millisecond)

		setButtonValue(ModeButtonBlueLED, 0)
		setButtonValue(ModeButtonGreenLED, 1)
		setButtonValue(ModeButtonRedLED, 0)
		time.Sleep(500 * time.Millisecond)

		setButtonValue(ModeButtonBlueLED, 0)
		setButtonValue(ModeButtonGreenLED, 0)
		setButtonValue(ModeButtonRedLED, 1)
		time.Sleep(500 * time.Millisecond)
	}
}

func setupButtonLED(button Button) {
	exportButton(button)
	setButtonDirection(button, "out")
}

func exportButton(button Button) {
	data := []byte(fmt.Sprintf("%v", button))
	ioutil.WriteFile("/sys/class/gpio/export", data, 0644)
}

func setButtonDirection(button Button, direction string) {
	data := []byte(direction)
	fname := fmt.Sprintf("/sys/class/gpio/gpio%d/direction", button)
	ioutil.WriteFile(fname, data, 0644)
}

func setButtonValue(button Button, value int) {
	data := []byte(fmt.Sprintf("%v", value))
	fname := fmt.Sprintf("/sys/class/gpio/gpio%d/value", button)
	ioutil.WriteFile(fname, data, 0644)
}

func unexportButton(button Button) {
	log.Printf("Unexporting %v\n", button)
	data := []byte(fmt.Sprintf("%v", button))
	ioutil.WriteFile("/sys/class/gpio/unexport", data, 0644)
}

func watchButton(button Button, out chan Button) {
	fname := fmt.Sprintf("/sys/class/gpio/gpio%d/value", button)

	exportButton(button)
	setButtonDirection(button, "in")

	defer unexportButton(button)

	v := false

	for {
		dat, err := ioutil.ReadFile(fname)
		check(err)

		if v && bytes.ContainsRune(dat, '0') {
			v = false
			out <- button
		} else if bytes.ContainsRune(dat, '1') {
			v = true
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func LEDSender(server string, in chan NeoPattern) {
	log.Println("Connecting to OPC")

	oc := opc.NewClient()
	err := oc.Connect("tcp", server)
	if err != nil {
		log.Fatal("Could not connect to Fadecandy server", err)
	}
	check(err)

	done := make(chan bool)

	for {
		log.Println("LEDSender: WATING FOR INPUT")
		pattern := <-in
		switch pattern {
		case PatternStop:
			log.Println("LEDSender: GOT STOP")
			done <- true
		case Pattern1:
			log.Println("LEDSender: GOT START PATTERN 1")
			go FCPattern1(oc, done)
		case Pattern2:
			log.Println("LEDSender: GOT START PATTERN 2")
			go FCPattern2(oc, done)

		}
	}

}

var Colors = []Color{
	{255, 0, 0},
	{255, 255, 0},
	{0, 255, 0},
	{0, 255, 255},
	{0, 0, 255},
	{255, 0, 255},
}

func FCClear(oc *opc.Client) {
	m := opc.NewMessage(0)
	m.SetLength(uint16(N_LEDS * 3))

	err := oc.Send(m)

	log.Println("FCClear")

	if err != nil {
		log.Println("couldn't send color", err)
	}
	check(err)
}

func FCPattern1(oc *opc.Client, done chan bool) {
	c1 := Colors[0]
	c2 := Color{0, 0, 0}

	for {
		for _, c := range Colors {

			if FCPattern1Loop(oc, &c1, &c2, done) {
				return
			}

			c2 = c1
			c1 = c

		}
	}

}

func FCPattern1Loop(oc *opc.Client, c1 *Color, c2 *Color, done chan bool) bool {
	ticker := time.NewTicker(time.Duration(10) * time.Millisecond)

	for i := 0; i < N_LEDS; i += 1 {
		select {
		case <-done:
			FCClear(oc)
			return true
		case <-ticker.C:
			FCPattern1Frame(oc, i, c1, c2)
		}
	}

	return false
}

func FCPattern1Frame(oc *opc.Client, frame int, c1 *Color, c2 *Color) {
	m := opc.NewMessage(0)
	m.SetLength(uint16(3 * N_LEDS))

	interval := 1

	for i := 0; i < frame; i += interval {
		m.SetPixelColor(i, c1.R, c1.G, c1.B)
	}

	for i := frame; i < N_LEDS; i += interval {
		m.SetPixelColor(i, c2.R, c2.G, c2.B)
	}

	err := oc.Send(m)

	if err != nil {
		log.Println("couldn't send color", err)
	}
	check(err)
}

func FCPattern2(oc *opc.Client, done chan bool) {
	// Starting with FCClear fixes the weird timing of the first frame
	FCClear(oc)

	for {
		for _, c := range Colors {
			if FCPattern2Loop(oc, &c, done) {
				return
			}
		}
	}
}

func FCPattern2Loop(oc *opc.Client, c *Color, done chan bool) bool {
	ticker := time.NewTicker(time.Duration(1800) * time.Millisecond)

	// Fade in the first color, but faster
	time.Sleep(200 * time.Millisecond)
	FCPattern2Frame(oc, c)

	select {
	case <-done:
		FCClear(oc)
		time.Sleep(10 * time.Millisecond)
		FCClear(oc)
		return true
	case <-ticker.C:

	}

	return false
}

func FCPattern2Frame(oc *opc.Client, c *Color) {
	m := opc.NewMessage(0)
	m.SetLength(uint16(3 * N_LEDS))

	for i := 0; i < N_LEDS; i++ {
		m.SetPixelColor(i, c.R, c.G, c.B)
	}

	err := oc.Send(m)

	if err != nil {
		log.Println("couldn't send color", err)
	}
	check(err)
}
