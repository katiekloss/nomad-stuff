package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	nomad "github.com/hashicorp/nomad/api"
	"github.com/simonfrey/jsonl"
)

type Transceiver struct {
	NodeId string
	Nomad  *nomad.Client
}

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

	// added in the job spec, eventually I'll see if I can add this to Nomad
	node_id := os.Getenv("NOMAD_NODE_ID")

	self := &Transceiver{
		NodeId: node_id,
		Nomad:  nomad_client,
	}

	allocs, _, err := nomad_client.Nodes().Allocations(node_id, &nomad.QueryOptions{})
	if err != nil {
		panic(err)
	}

	var max_alloc_index uint64
	for _, alloc := range allocs {
		if max_alloc_index < alloc.ModifyIndex {
			max_alloc_index = alloc.ModifyIndex
		}

		if alloc.ClientStatus != "pending" && alloc.ClientStatus != "running" {
			continue
		}

		go self.logAlloc(alloc)
	}

	self.watchForNewAllocs(max_alloc_index)
}

func (transceiver *Transceiver) watchForNewAllocs(from uint64) {
	events, err := transceiver.Nomad.EventStream().Stream(
		context.Background(),
		map[nomad.Topic][]string{
			nomad.TopicAllocation: {"*"}, // can remove the wildcard in nomad 1.11.1
		},
		0,
		&nomad.QueryOptions{})

	if err != nil {
		panic(err)
	}

	for frame := range events {
		for _, ev := range frame.Events {
			if ev.Index <= from || ev.Type != "AllocationUpdated" { // or if it's not an allocation event, if we start listening to others
				continue
			}

			alloc, _ := ev.Allocation()
			if alloc.TaskStates == nil {
				// This gets fired without any tasks when the allocation is first scheduled
				continue
			} else if alloc.NodeID != transceiver.NodeId {
				continue
			}

			log.Printf("Logging new alloc %s", alloc.ID)
			transceiver.logAlloc(alloc)
		}
	}
}

func (transceiver *Transceiver) logAlloc(alloc *nomad.Allocation) {
	cancel := make(chan struct{})

	for task_name, task := range alloc.TaskStates {
		// skip prestart tasks which won't be restarted
		if task.State == "dead" && !task.Failed {
			continue
		}

		go transceiver.logAllocTask(alloc, task_name, "stdout", &cancel)
		go transceiver.logAllocTask(alloc, task_name, "stderr", &cancel)
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

func (transceiver *Transceiver) logAllocTask(alloc *nomad.Allocation, taskName string, logName string, cancel *chan struct{}) {

	//http := &http.Client{}
	//vlService := getServiceByName(nomad_client, "victorialogs")
	//targetUrl := fmt.Sprintf("http://%s:%d/insert/jsonline?_stream_fields=nomad.alloc,nomad.job", vlService.Address, vlService.Port)

	taskLogs, errs := transceiver.Nomad.AllocFS().Logs(alloc, true, taskName, logName, "end", 0, *cancel, &nomad.QueryOptions{})

	log.Printf("Attached to %s:%s:%s", alloc.ID, taskName, logName)

	iterateLogFrames := func(logs <-chan *nomad.StreamFrame) {
		for frame := range logs {
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
					Type: "transceiver",
				},
			}

			buf := &bytes.Buffer{}
			jsonl.NewWriter(buf).Write(line)
			log.Println(buf.String())
		}
	}

	//
	//resp, err := http.Post(targetUrl, "application/stream+json", bytes.NewReader(buf.Bytes()))

	// if err != nil {
	// 	panic(err)
	// }

	// if resp.StatusCode != 200 {
	// 	log.Println(resp)
	// }

	go iterateLogFrames(taskLogs)

	go func() {
		var restart bool
		for e := range errs {
			var err nomad.UnexpectedResponseError
			if errors.As(e, &err) && err.StatusCode() == 404 {
				restart = true
				break
			} else {
				log.Printf("Error streaming from %s:%s:%s: %s", alloc.ID, taskName, logName, err)
			}
		}

		if restart {
			log.Printf("Task %s isn't started yet, re-attaching log listener?", taskName)
			// i don't know go, does this introduce the possibility of a stack overflow?
			// go logAllocTask(nomad_client, alloc, taskName, logName, cancel)
		}
	}()
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
