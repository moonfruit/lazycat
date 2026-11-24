package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func macvtapMajor(devices string) (int, error) {
	var characters bool
	for line := range strings.SplitSeq(devices, "\n") {
		switch strings.TrimSpace(line) {
		case "Character devices:":
			characters = true
		case "Block devices:":
			characters = false
		}
		fields := strings.Fields(line)
		if characters && len(fields) == 2 && fields[1] == "macvtap" {
			major, err := strconv.Atoi(fields[0])
			if err == nil && major > 0 {
				return major, nil
			}
			return 0, fmt.Errorf("invalid macvtap major %q", fields[0])
		}
	}
	return 0, fmt.Errorf("macvtap character device not registered")
}

func hostMacvtapMajor() (int, error) {
	b, err := os.ReadFile("/proc/devices")
	if err != nil {
		return 0, err
	}
	return macvtapMajor(string(b))
}
