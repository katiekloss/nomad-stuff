package main

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"

	nomad "github.com/hashicorp/nomad/api"
	"github.com/simonfrey/jsonl"
)

func main() {
	var headers = map[string][]string{
		"X-Nomad-Token": {os.Getenv("NOMAD_TOKEN")},
	}

	nomad_client, err := nomad.NewClient(&nomad.Config{
		Address: os.Getenv("NOMAD_UNIX_ADDR"),
		Headers: headers,
	})

	if err != nil {
		panic(err)
	}

	this_node, err := nomad_client.Agent().Self()
	if err != nil {
		panic(err)
	}

	allocs, _, err := nomad_client.Allocations().List(&nomad.QueryOptions{})
	if err != nil {
		panic(err)
	}

	wg := &sync.WaitGroup{}

	for _, alloc := range allocs {
		if alloc.ClientStatus != "pending" && alloc.ClientStatus != "running" {
			continue
		}

		// ignore allocs not on this client (there has to be an easier way to get this ID)
		if alloc.NodeID != this_node.Stats["client"]["node_id"] {
			continue
		}

		job, _, err := nomad_client.Jobs().Info(alloc.JobID, &nomad.QueryOptions{})
		if err != nil {
			log.Printf("No access to %s job: %s", *job.ID, err.Error())
			continue
		} else if *job.ID == "receiver" {
			continue
		}

		wg.Add(1)
		go logAlloc(nomad_client, alloc, wg)
	}

	wg.Wait()
}

func logAlloc(nomad_client *nomad.Client, stub *nomad.AllocationListStub, waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()

	cancel := make(chan struct{})
	alloc, _, err := nomad_client.Allocations().Info(stub.ID, &nomad.QueryOptions{})
	if err != nil {
		panic(err)
	}

	for task_name := range alloc.TaskStates {
		waitGroup.Add(2)
		go logAllocTask(nomad_client, alloc, task_name, "stdout", &cancel, waitGroup)
		go logAllocTask(nomad_client, alloc, task_name, "stderr", &cancel, waitGroup)
	}
}

type NomadMeta struct {
	Alloc string `json:"alloc"`
	Job   string `json:"job"`
	Group string `json:"group"`
	Task  string `json:"task"`
	Node  string `json:"node"`
}

type AgentMeta struct {
	Type string `json:"type"`
}

type LogLine struct {
	Msg   string     `json:"_msg"`
	Time  string     `json:"_time"`
	Pipe  string     `json:"pipe"`
	Nomad *NomadMeta `json:"nomad"`
	Agent *AgentMeta `json:"agent"`
}

func logAllocTask(nomad_client *nomad.Client, alloc *nomad.Allocation, taskName string, logName string, cancel *chan struct{}, waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()

	http := &http.Client{}
	vlService := getServiceByName(nomad_client, "victorialogs")
	targetUrl := fmt.Sprintf("http://%s:%s/insert/jsonline?_stream_fields=nomad.alloc,nomad.job", vlService.Address, &vlService.Port)

	taskLogs, _ := nomad_client.AllocFS().Logs(alloc, true, taskName, logName, "end", 0, *cancel, &nomad.QueryOptions{})

	log.Printf("Attached to %s:%s:%s", alloc.ID, taskName, logName)

	for frame := range taskLogs {
		line := &LogLine{
			Msg:  string(frame.Data),
			Time: "0",
			Pipe: logName,
			Nomad: &NomadMeta{
				Alloc: alloc.ID,
				Job:   alloc.JobID,
				Group: alloc.TaskGroup,
				Task:  taskName,
				Node:  alloc.NodeID,
			},
			Agent: &AgentMeta{
				Type: "receiver",
			},
		}

		buf := &bytes.Buffer{}
		jsonl.NewWriter(buf).Write(line)

		resp, err := http.Post(targetUrl, "application/stream+json", bytes.NewReader(buf.Bytes()))

		if err != nil {
			panic(err)
		}

		if resp.StatusCode != 200 {
			log.Println(resp)
		}
	}
}

func getServiceByName(nomad_client *nomad.Client, name string) *nomad.ServiceRegistration {
	services, _, err := nomad_client.Services().Get(name, &nomad.QueryOptions{})
	if err != nil {
		panic(err)
	}

	if len(services) == 0 {
		panic(fmt.Sprintf("Can't find service %s", name))
	}

	return services[0]
}
