package main

import (
	"context"
	"log"
	"os"

	nomad "github.com/hashicorp/nomad/api"
)

func main() {
	var current_state = make(map[string]*nomad.Job)

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

	// test client API access
	_, _, err = nomad_client.Allocations().Info(os.Getenv("NOMAD_ALLOC_ID"), &nomad.QueryOptions{})
	if err != nil {
		panic(err)
	}

	jobs, _, err := nomad_client.Jobs().List(&nomad.QueryOptions{})
	if err != nil {
		panic(err)
	}

	var max_index int
	for _, job := range jobs {
		current_state[job.ID], _, err = nomad_client.Jobs().Info(job.ID, &nomad.QueryOptions{})
		if err != nil {
			panic(err)
		}
		log.Printf("Registered %s", job.Name)
		if max_index < int(job.ModifyIndex) {
			max_index = int(job.ModifyIndex)
		}
	}

	ev, err := nomad_client.EventStream().Stream(
		context.Background(),
		map[nomad.Topic][]string{
			nomad.TopicJob: {}},
		0,
		&nomad.QueryOptions{})

	if err != nil {
		panic(err)
	}

	for frame := range ev {
		if frame.IsHeartbeat() {
			continue
		}

		for _, e := range frame.Events {
			if e.Topic == nomad.TopicJob {
				job, _ := e.Job()

				if e.Index <= uint64(max_index) {
					log.Printf("Skipping event %d (payload is older than snapshot taken at index %d)", e.Index, max_index)
					continue
				}

				last_state := current_state[*job.ID]
				current_state[*job.Name] = job

				if *last_state.Status == *job.Status {
					continue
				}

				if *last_state.Stop != *job.Stop {
					var change string
					if *job.Stop {
						change = "stopped"
					} else {
						change = "started"
					}

					log.Printf("Job %s %s", *job.Name, change)
					continue
				}

				log.Printf("Job %s is %s (was %s)", *job.Name, *job.Status, *last_state.Status)
			}
		}
	}
}
