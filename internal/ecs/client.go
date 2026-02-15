// Package ecs provides a client for managing ECS Fargate service scaling.
package ecs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
)

// API is the subset of the ECS API the autoscaler needs.
type API interface {
	DescribeServices(ctx context.Context, input *ecs.DescribeServicesInput, opts ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
	UpdateService(ctx context.Context, input *ecs.UpdateServiceInput, opts ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error)
	ListTasks(ctx context.Context, input *ecs.ListTasksInput, opts ...func(*ecs.Options)) (*ecs.ListTasksOutput, error)
	DescribeTasks(ctx context.Context, input *ecs.DescribeTasksInput, opts ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
	UpdateTaskProtection(ctx context.Context, input *ecs.UpdateTaskProtectionInput, opts ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error)
}

// TaskInfo holds an ECS task's ARN and private IP.
type TaskInfo struct {
	TaskArn   string
	PrivateIP string
}

// Client wraps ECS API access for the autoscaler.
type Client struct {
	cluster string
	service string
	api     API
}

// New creates a new ECS client using the default AWS config.
func New(ctx context.Context, cluster, service string) (*Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("loading AWS config: %w", err)
	}

	return &Client{
		cluster: cluster,
		service: service,
		api:     ecs.NewFromConfig(cfg),
	}, nil
}

// GetServiceStatus returns the desired and running task counts for the service.
func (c *Client) GetServiceStatus(ctx context.Context) (desired, running int32, err error) {
	out, err := c.api.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(c.cluster),
		Services: []string{c.service},
	})
	if err != nil {
		return 0, 0, fmt.Errorf("describing service: %w", err)
	}

	if len(out.Services) == 0 {
		return 0, 0, fmt.Errorf("service %s not found in cluster %s", c.service, c.cluster)
	}

	svc := out.Services[0]
	return svc.DesiredCount, svc.RunningCount, nil
}

// SetDesiredCount updates the desired task count for the service.
func (c *Client) SetDesiredCount(ctx context.Context, count int32) error {
	_, err := c.api.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      aws.String(c.cluster),
		Service:      aws.String(c.service),
		DesiredCount: aws.Int32(count),
	})
	if err != nil {
		return fmt.Errorf("updating service desired count: %w", err)
	}

	return nil
}

// GetTaskIPs returns the ARN and private IP of each task in the service.
func (c *Client) GetTaskIPs(ctx context.Context) ([]TaskInfo, error) {
	listOut, err := c.api.ListTasks(ctx, &ecs.ListTasksInput{
		Cluster:     aws.String(c.cluster),
		ServiceName: aws.String(c.service),
	})
	if err != nil {
		return nil, fmt.Errorf("listing tasks: %w", err)
	}

	if len(listOut.TaskArns) == 0 {
		return nil, nil
	}

	descOut, err := c.api.DescribeTasks(ctx, &ecs.DescribeTasksInput{
		Cluster: aws.String(c.cluster),
		Tasks:   listOut.TaskArns,
	})
	if err != nil {
		return nil, fmt.Errorf("describing tasks: %w", err)
	}

	var tasks []TaskInfo
	for _, task := range descOut.Tasks {
		info := TaskInfo{TaskArn: aws.ToString(task.TaskArn)}
		for _, att := range task.Attachments {
			if aws.ToString(att.Type) == "ElasticNetworkInterface" {
				for _, detail := range att.Details {
					if aws.ToString(detail.Name) == "privateIPv4Address" {
						info.PrivateIP = aws.ToString(detail.Value)
					}
				}
			}
		}
		tasks = append(tasks, info)
	}

	return tasks, nil
}

// SetTaskProtection enables or disables scale-in protection for the given tasks.
func (c *Client) SetTaskProtection(ctx context.Context, taskArns []string, enabled bool, expiresInMinutes int32) error {
	const batchSize = 10

	for i := 0; i < len(taskArns); i += batchSize {
		end := i + batchSize
		if end > len(taskArns) {
			end = len(taskArns)
		}

		input := &ecs.UpdateTaskProtectionInput{
			Cluster:            aws.String(c.cluster),
			Tasks:              taskArns[i:end],
			ProtectionEnabled:  enabled,
		}
		if enabled && expiresInMinutes > 0 {
			input.ExpiresInMinutes = aws.Int32(expiresInMinutes)
		}

		_, err := c.api.UpdateTaskProtection(ctx, input)
		if err != nil {
			return fmt.Errorf("updating task protection: %w", err)
		}
	}

	return nil
}
