package main

import (
	"os"
	"context"
	"github.com/docker/docker/client"
	 "github.com/docker/docker/api/types"     // Docker API networking types library
	"errors"
	"log"
	"fmt"
	"time"
	"strings"
	"encoding/json"
	"io"

)
func InitDockerClient() (*client.Client, error) {
	// use default location unless env set
	if len(os.Getenv("DOCKER_HOST")) == 0 {
		if err := os.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock"); err != nil {
			return nil, err
		}
	}

	return client.NewEnvClient()
}

func main () {
	if len(os.Args)==1 {
		fmt.Printf("Usage: PROG containerId")
	}
	myClient, err := InitDockerClient()
	if err != nil{
		log.Fatalf("Error initialize docker client %s ", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*3)
	defer cancel ()

	stats:=ContainerStats(ctx, myClient, os.Args[1])
	fmt.Printf("Max values aha\n%s\n", stats)
}
type StatsEntry struct {
	CPUPercentage float64 `json:"cpuPercent"`
	Memory float64	`json:"memory"`
	BlockRead float64 `json:"blockRead"`
	BlockWrite float64 `json:"blockWrite"`
}
func (s StatsEntry) String() string {
	pretty, _ := json.MarshalIndent(&s, "", "\t")
	return string(pretty)
}

func (c *StatsEntry) SetMaxStats (s StatsEntry) {
	//	fmt.Printf ("Got %s\n", s)
	if c.CPUPercentage < s.CPUPercentage {
		c.CPUPercentage = s.CPUPercentage
	}
	if c.Memory < s.Memory {
		c.Memory = s.Memory
	}
	if c.BlockRead < s.BlockRead {
		c.BlockRead = s.BlockRead
	}
	if c.BlockWrite < s.BlockWrite {
		c.BlockWrite = s.BlockWrite
	}
}
func ContainerStats (ctx context.Context, cli *client.Client, containerId string) StatsEntry{
	var MaxStats = StatsEntry{}

	errChan              := make(chan error, 1)
	doneChan			:= make (chan bool)
	go collect (ctx, containerId, cli, true, &MaxStats, doneChan, errChan)
	for {
		select {
		case <-doneChan:
			return MaxStats
		case <-errChan:
			return MaxStats
		default:
			continue
		}
	}

	return MaxStats
}
func collect(ctx context.Context, containerId string,cli client.APIClient, streamStats bool, res *StatsEntry, doneChan chan bool, errChan chan error) {

	//let's look at the container stats
	var (
		previousCPU    uint64
		previousSystem uint64


	)
	timeout := false
	// is msgCtx closes, stop trying
	go func() {
		<-ctx.Done()
		timeout = true
	}()

	response, err := cli.ContainerStats(ctx, containerId, streamStats)
	if err != nil {
		errChan <- err
		return
	}
	defer response.Body.Close()

	dec := json.NewDecoder(response.Body)
	for {
		fmt.Printf("-")
		if timeout {
			// only if imgCtx is closed
			endErr := errors.New("Request timed out")
			errChan <- endErr
			return
		}
		var (
			v                 *types.StatsJSON
			cpuPercent        = 0.0
			blkRead, blkWrite uint64 // Only used on Linux
			mem               = 0.0
		)

		i:=0
		if err := dec.Decode(&v); err != nil {
			fmt.Printf("Err :%s\n",err.Error())
			dec = json.NewDecoder(io.MultiReader(dec.Buffered(), response.Body))
			if err == io.EOF {
				doneChan <- true
				return
			} else {
				if i>100 {
					fmt.Printf("Got more error than I like: err=%s\n", err.Error())
					errChan <- err
					return
				}
				i++
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		previousCPU = v.PreCPUStats.CPUUsage.TotalUsage
		previousSystem = v.PreCPUStats.SystemUsage
		cpuPercent = calculateCPUPercent(previousCPU, previousSystem, v)
		blkRead, blkWrite = calculateBlockIO(v.BlkioStats)
		mem = float64(v.MemoryStats.Usage)

		res.SetMaxStats(StatsEntry{
			CPUPercentage:    cpuPercent,
			Memory:           mem,
			BlockRead:        float64(blkRead),
			BlockWrite:       float64(blkWrite),
		})
		if !streamStats {
			doneChan <- true
			return
		}
	}
}


func calculateCPUPercent(previousCPU, previousSystem uint64, v *types.StatsJSON) float64 {
	var (
		cpuPercent = 0.0
		// calculate the change for the cpu usage of the container in between readings
		cpuDelta = float64(v.CPUStats.CPUUsage.TotalUsage) - float64(previousCPU)
		// calculate the change for the entire system between readings
		systemDelta = float64(v.CPUStats.SystemUsage) - float64(previousSystem)
	)

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * float64(len(v.CPUStats.CPUUsage.PercpuUsage)) * 100.0
	}
	return cpuPercent
}

func calculateBlockIO(blkio types.BlkioStats) (blkRead uint64, blkWrite uint64) {
	for _, bioEntry := range blkio.IoServiceBytesRecursive {
		switch strings.ToLower(bioEntry.Op) {
		case "read":
			blkRead = blkRead + bioEntry.Value
		case "write":
			blkWrite = blkWrite + bioEntry.Value
		}
	}
	return
}
/*
func main () {
	image := "test"
	hostPayloadLocation := "/Users/home/testdata/misc/docker"
	network := "aiware_default"
	var networkMode  dockerContainerTypes.NetworkMode=dockerContainerTypes.NetworkMode(network)

	myClient, err := InitDockerClient()
	if err != nil{
		log.Fatalf("Error initialize docker client %s ", err)
	}
	logs := make([]string, 0)

	// tricky:  need to save the taskpayload to a file then mount it in the host.
	// for now assume that it's there
	//tmpfile := "/var/payload.json"
	// create container, see https://godoc.org/github.com/moby/moby/client#Client.ContainerCreate

	envVars := []string{
		fmt.Sprintf("PAYLOAD_FILE=/var/payload.json"),
	}

	cfg := &dockerContainerTypes.Config{
		Image: image,
		Env:        envVars,
	}
	hostCfg := &dockerContainerTypes.HostConfig{
		AutoRemove: true,
		NetworkMode: networkMode,
		Binds: []string{
			fmt.Sprintf("%s/payload.json:/var/payload.json", hostPayloadLocation),
		},
	}
	created, err := myClient.ContainerCreate(context.Background(), cfg, hostCfg, &dockerNetworkTypes.NetworkingConfig{}, "MYBIGFATCONTAINER")
	if err != nil {
		return logs, ExitCodeError, err
	}

	// run container, see https://godoc.org/github.com/moby/moby/client#Client.ContainerStart
	err = myClient.ContainerStart(msgCtx, created.ID, dockerTypes.ContainerStartOptions{})
	if err != nil {
		return logs, ExitCodeError, err
	}
	// stop container, see https://godoc.org/github.com/moby/moby/client#Client.ContainerStop
	immediate := time.Duration(0)
	defer myClient.ContainerStop(msgCtx, created.ID, &immediate)

	// wait for container done
	done := make(chan int, 1)
	errChan := make(chan error, 1)
	go WaitForContainerDone(myClient, msgCtx, created.ID, done, errChan)


	// get logs, see https://godoc.org/github.com/moby/moby/client#Client.ContainerLogs
	logOpts := dockerTypes.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	}
	logReader, err := myClient.ContainerLogs(msgCtx, created.ID, logOpts)
	if err != nil {
		return logs, netLogs, ExitCodeError, err
	}
	defer logReader.Close()

	logScanner := bufio.NewScanner(logReader)
	line := make(chan string, 1)
	go func() {
		for logScanner.Scan() {
			line <- logScanner.Text()
		}
	}()
	for {
		select {
		case log := <-line:
			logs = append(logs, base.DecodeLogLine(log))
		case netLog := <-networkLog:
			netLogs = append(netLogs, strings.TrimSpace(netLog))
		case code := <-done:
			return logs, netLogs, code, nil
		case err = <-errChan:
			return logs, netLogs, ExitCodeError, err
		default:
			continue
		}
	}

	return logs, netLogs, ExitCodeError, nil
}
*/