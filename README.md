# My Nomad Stuff
This has a few pet projects I use for monitoring my [Nomad](https://developer.hashicorp.com/nomad) cluster.

## sentry
Monitors job (and eventually allocation) events within the cluster and reports notable ones via Shoutrr.

## receiver
Ships logs from allocs to a central sink, without adding sidecars to individual jobs.
