package main

import "strconv"

type workerConfig struct {
	PipeName string
	Binary   string
	HostPID  uint32
}

func (c workerConfig) args(worker bool) []string {
	args := []string{"--child", "--pipe", c.PipeName}
	if c.Binary != "" {
		args = append(args, "--agent-desktop-bin", c.Binary)
	}
	if c.HostPID != 0 {
		args = append(args, "--host-pid", strconv.FormatUint(uint64(c.HostPID), 10))
	}
	if worker {
		args = append(args, "--worker")
	}
	return args
}
