package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"slices"
	"sync"

	"github.com/alecthomas/kong"
	"github.com/samber/lo"
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

type SaveProfileCmd struct {
	Name string `arg:"1" help:"The name of the profile."`
}

type LoadProfileCmd struct {
	Name string `arg:"1" help:"The name of the profile."`
}

type CLI struct {
	Save SaveProfileCmd `cmd:"1" help:"Save the current profile to a file."`
	Load LoadProfileCmd `cmd:"1" help:"Load the profile from a file."`
}

func (cmd SaveProfileCmd) Run() error {
	result, err := currentScreenSetup()
	if err != nil {
		return fmt.Errorf("failed to load current screen setup: %w", err)
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
			return fmt.Errorf("failed to determine refreshrate for output %s", output.Name)
		}

		profile.Screens = append(profile.Screens, screen)
	}

	b, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("failed to serialize profile: %w", err)
	}
	if err := os.WriteFile(cmd.Name, b, 0644); err != nil {
		return fmt.Errorf("failed to write profile: %w", err)
	}

	return nil
}

func (cmd LoadProfileCmd) Run() error {
	b, err := os.ReadFile(cmd.Name)
	if err != nil {
		return fmt.Errorf("failed to read profile: %w", err)
	}
	var profile Profile
	if err := json.Unmarshal(b, &profile); err != nil {
		return fmt.Errorf("failed to deserialize profile: %w", err)
	}

	currentScreen, err := currentScreenSetup()
	if err != nil {
		return fmt.Errorf("failed to load current screen setup: %w", err)
	}

	outputByName := lo.Associate(currentScreen.Outputs, func(output Output) (string, Output) {
		return output.Name, output
	})

	type targetOutputProperties struct {
		name     string
		mode     string
		position string
		scale    string
	}
	var targetOutputs []targetOutputProperties
	var targetOutputNames = make(map[string]bool)
	for _, desiredScreen := range profile.Screens {
		var targetOutput targetOutputProperties
		targetOutput.name = desiredScreen.Name
		targetOutput.scale = fmt.Sprintf("%f", desiredScreen.Scale)
		targetOutput.position = fmt.Sprintf("%d,%d", desiredScreen.Position.X, desiredScreen.Position.Y)

		output, exists := outputByName[desiredScreen.Name]
		if !exists {
			return fmt.Errorf("profile references missing output %s", desiredScreen.Name)
		}

		potentialModes := lo.Filter(output.Modes, func(mode Mode, _ int) bool {
			return mode.Size == desiredScreen.Size
		})
		if len(potentialModes) == 0 {
			return fmt.Errorf("output %s doesn't contain a matching mode", desiredScreen.Name)
		}
		// Pick the mode with the next best refreshrate
		slices.SortFunc(potentialModes, func(a, b Mode) int {
			diffA := math.Abs(desiredScreen.RefreshRate - a.RefreshRate)
			diffB := math.Abs(desiredScreen.RefreshRate - b.RefreshRate)

			return int(diffA - diffB)
		})
		targetOutput.mode = potentialModes[0].Name

		targetOutputs = append(targetOutputs, targetOutput)
		targetOutputNames[targetOutput.name] = true
	}

	var disabledOutputs []string
	for outputName := range outputByName {
		if !targetOutputNames[outputName] {
			disabledOutputs = append(disabledOutputs, outputName)
		}
	}

	var args []string
	for _, outputName := range disabledOutputs {
		args = append(args, fmt.Sprintf("output.%s.disable", outputName))
	}
	for _, output := range targetOutputs {
		args = append(args,
			fmt.Sprintf("output.%s.enable", output.name),
			fmt.Sprintf("output.%s.mode.%s", output.name, output.mode),
			fmt.Sprintf("output.%s.position.%s", output.name, output.position),
			fmt.Sprintf("output.%s.scale.%s", output.name, output.scale),
		)
	}

	return exec.Command("kscreen-doctor", args...).Run()
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

	wg.Wait()

	if decodeError != nil {
		return KScreenDoctorResult{}, fmt.Errorf("failed to decode kscreen-doctor result: %w", decodeError)
	}

	return result, nil
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli, kong.Name("kdedisplayprofile"))
	ctx.FatalIfErrorf(ctx.Error)

	ctx.FatalIfErrorf(ctx.Run())
}
