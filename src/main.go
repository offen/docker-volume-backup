package main

import (
	"context"
	"fmt"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load("/etc/backup.env"); err != nil {
		panic(err)
	}

	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		panic(err)
	}

	socketExists, err := fileExists("/var/run/docker.sock")
	if err != nil {
		panic(err)
	}

	var containersToStop []types.Container
	if socketExists {
		containersToStop, err = cli.ContainerList(ctx, types.ContainerListOptions{
			Quiet: true,
			Filters: filters.NewArgs(filters.KeyValuePair{
				Key:   "label",
				Value: fmt.Sprintf("docker-volume-backup.stop-during-backup=%s", os.Getenv("BACKUP_STOP_CONTAINER_LABEL")),
			}),
		})
		if err != nil {
			panic(err)
		}
		fmt.Printf("Stopping %d containers\n", len(containersToStop))
	}

	if len(containersToStop) != 0 {
		fmt.Println("Stopping containers")
		for _, container := range containersToStop {
			if err := cli.ContainerStop(ctx, container.ID, nil); err != nil {
				panic(err)
			}
		}
	}

	fmt.Println("Creating backup")
	// TODO: Implement backup

	if len(containersToStop) != 0 {
		fmt.Println("Starting containers/services back up")
		servicesRequiringUpdate := map[string]struct{}{}
		for _, container := range containersToStop {
			if swarmServiceName, ok := container.Labels["com.docker.swarm.service.name"]; ok {
				servicesRequiringUpdate[swarmServiceName] = struct{}{}
				continue
			}
			if err := cli.ContainerStart(ctx, container.ID, types.ContainerStartOptions{}); err != nil {
				panic(err)
			}
		}

		if len(servicesRequiringUpdate) != 0 {
			services, _ := cli.ServiceList(ctx, types.ServiceListOptions{})
			for serviceName := range servicesRequiringUpdate {
				var serviceMatch swarm.Service
				for _, service := range services {
					if service.Spec.Name == serviceName {
						serviceMatch = service
						break
					}
				}
				if serviceMatch.ID == "" {
					panic(fmt.Sprintf("Couldn't find service with name %s", serviceName))
				}
				serviceMatch.Spec.TaskTemplate.ForceUpdate = 1
				cli.ServiceUpdate(
					ctx, serviceMatch.ID,
					serviceMatch.Version, serviceMatch.Spec, types.ServiceUpdateOptions{},
				)
			}
		}
	}
}

func fileExists(location string) (bool, error) {
	_, err := os.Stat(location)
	if err != nil && err != os.ErrNotExist {
		return false, err
	}
	return err == nil, nil
}
