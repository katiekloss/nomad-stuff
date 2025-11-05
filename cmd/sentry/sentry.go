package main

import (
	"log"
	"os"
	"time"

	nomad "github.com/hashicorp/nomad/api"
)

func main() {
	var current_state = make(map[string]*nomad.JobListStub)

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

	for {
		jobs, _, err := nomad_client.Jobs().List(&nomad.QueryOptions{})
		if err != nil {
			panic(err)
		}

		for _, job := range jobs {
			if _, present := current_state[job.Name]; !present {
				log.Printf("Registered %s", job.Name)
				current_state[job.Name] = job
				continue
			}

			last_state := current_state[job.Name]
			current_state[job.Name] = job

			if last_state.Status == job.Status {
				continue
			}

			if last_state.Stop != job.Stop {
				var change string
				if job.Stop {
					change = "stopped"
				} else {
					change = "started"
				}

				log.Printf("Job %s %s", job.Name, change)
				continue
			}

			log.Printf("Job %s is %s (was %s)", job.Name, job.Status, last_state.Status)
		}

		time.Sleep(5 * time.Second)
	}
}
