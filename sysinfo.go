package main

import (
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

type SystemSpecs struct {
	CPU         string `json:"cpu"`
	RAM         uint64 `json:"ram"`          // In GB
	VRAM        uint64 `json:"vram"`         // In GB
	GPU         string `json:"gpu"`
	OS          string `json:"os"`
	BalancedGPU int    `json:"balanced_gpu"` // Recommended GPU layers
}

func GetSystemSpecs() (SystemSpecs, error) {
	specs := SystemSpecs{
		OS: runtime.GOOS,
	}

	if runtime.GOOS == "windows" {
		return getWindowsSpecs(specs)
	}

	// Fallback for non-windows (minimal)
	specs.CPU = "Unknown (Non-Windows)"
	return specs, nil
}

func runWmic(args ...string) ([]byte, error) {
	cmd := exec.Command("wmic", args...)
	cmd.SysProcAttr = getSysProcAttr()
	return cmd.Output()
}

func getWindowsSpecs(specs SystemSpecs) (SystemSpecs, error) {
	// CPU
	out, _ := runWmic("cpu", "get", "name")
	lines := strings.Split(string(out), "\n")
	if len(lines) > 1 {
		specs.CPU = strings.TrimSpace(lines[1])
	}

	// Total RAM
	out, _ = runWmic("computersystem", "get", "totalphysicalmemory")
	lines = strings.Split(string(out), "\n")
	if len(lines) > 1 {
		memStr := strings.TrimSpace(lines[1])
		mem, _ := strconv.ParseUint(memStr, 10, 64)
		specs.RAM = mem / (1024 * 1024 * 1024)
	}

	// GPU and VRAM
	// Note: wmic might return multiple GPUs, we try to find the one with most VRAM or just the first dedicated one
	out, _ = runWmic("path", "win32_VideoController", "get", "name,AdapterRAM")
	lines = strings.Split(string(out), "\n")
	if len(lines) > 1 {
		// Example output:
		// AdapterRAM  Name
		// 4294967296  NVIDIA GeForce GT 1030

		for _, line := range lines[1:] {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				vramStr := parts[0]
				gpuName := strings.Join(parts[1:], " ")
				vram, _ := strconv.ParseUint(vramStr, 10, 64)

				// Usually dedicated GPUs have > 1GB and we take the first one found or highest
				vramGB := vram / (1024 * 1024 * 1024)
				if vramGB > specs.VRAM {
					specs.VRAM = vramGB
					specs.GPU = gpuName
				}
			}
		}
	}

	return specs, nil
}

func (s *SystemSpecs) CalculateBalancedGPU(modelSizeGB float64) int {
	if s.VRAM == 0 {
		return 0
	}

	// Very rough heuristic:
	// A typical 7B model is ~5GB. 32 layers.
	// If VRAM is 2GB, we can offload roughly 2/5 of layers.
	// We should leave some VRAM for the system (0.5GB - 1GB)

	availableVRAM := float64(s.VRAM) - 0.5
	if availableVRAM < 0 {
		availableVRAM = 0
	}

	if modelSizeGB == 0 {
		return 0
	}

	ratio := availableVRAM / modelSizeGB
	if ratio > 1 {
		ratio = 1
	}

	// Assume 32 layers as a baseline for most small models
	layers := int(ratio * 32)
	if layers > 32 {
		layers = 32
	}

	return layers
}
