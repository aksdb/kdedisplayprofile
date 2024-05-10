package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"slices"
	"sync"
)

type Output struct {
	Name          string   `json:"name"`
	CurrentModeId string   `json:"currentModeId"`
	Enabled       bool     `json:"enabled"`
	Size          Size     `json:"size"`
	Pos           Position `json:"pos"`
	Scale         float64  `json:"scale"`
	Modes         []Mode   `json:"modes"`
	Priority      int      `json:"priority"`
}

type Mode struct {
	Id          string  `json:"id"`
	Name        string  `json:"name"`
	RefreshRate float64 `json:"refreshRate"`
	Size        Size    `json:"size"`
}

type Size struct {
	Height int `json:"height"`
	Width  int `json:"width"`
}

type Position struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type KScreenDoctorResult struct {
	Outputs []Output `json:"outputs"`
}

type Profile struct {
	Screens []Screen `json:"screens"`
}

type Screen struct {
	Name        string   `json:"name"`
	Size        Size     `json:"size"`
	Position    Position `json:"position"`
	RefreshRate float64  `json:"refreshRate"`
	Scale       float64  `json:"scale"`
}

func main() {
	result, err := currentScreenSetup()
	if err != nil {
		log.Fatal(err)
	}

	// Sort by priority.
	slices.SortFunc(result.Outputs, func(a, b Output) int {
		return a.Priority - b.Priority
	})

	var profile Profile
	for _, output := range result.Outputs {
		if !output.Enabled {
			continue
		}

		var screen Screen
		screen.Name = output.Name
		screen.Size = output.Size
		screen.Position = output.Pos
		screen.Scale = output.Scale

		for _, mode := range output.Modes {
			if mode.Id == output.CurrentModeId {
				screen.RefreshRate = mode.RefreshRate
				break
			}
		}

		if screen.RefreshRate == 0 {
			log.Fatalf("failed to determine refreshrate for output %s\n", output.Name)
		}

		profile.Screens = append(profile.Screens, screen)
	}

	fmt.Println(profile)
}

func currentScreenSetup() (KScreenDoctorResult, error) {
	cmd := exec.Command("kscreen-doctor", "--json")
	output, err := cmd.StdoutPipe()
	if err != nil {
		return KScreenDoctorResult{}, fmt.Errorf("failed to pipe kscreen-doctor: %w", err)
	}
	defer output.Close()

	var result KScreenDoctorResult
	var decodeError error

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		decodeError = json.NewDecoder(output).Decode(&result)
		defer wg.Done()
	}()

	if err := cmd.Run(); err != nil {
		return KScreenDoctorResult{}, fmt.Errorf("failed to run kscreen-doctor: %w", err)
	}

	if decodeError != nil {
		return KScreenDoctorResult{}, fmt.Errorf("failed to decode kscreen-doctor result: %w", decodeError)
	}

	return result, nil
}
