package ecs

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

type mockECSAPI struct {
	describeServicesFn     func(ctx context.Context, input *ecs.DescribeServicesInput, opts ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
	updateServiceFn        func(ctx context.Context, input *ecs.UpdateServiceInput, opts ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error)
	listTasksFn            func(ctx context.Context, input *ecs.ListTasksInput, opts ...func(*ecs.Options)) (*ecs.ListTasksOutput, error)
	describeTasksFn        func(ctx context.Context, input *ecs.DescribeTasksInput, opts ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error)
	updateTaskProtectionFn func(ctx context.Context, input *ecs.UpdateTaskProtectionInput, opts ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error)
}

func (m *mockECSAPI) DescribeServices(ctx context.Context, input *ecs.DescribeServicesInput, opts ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	return m.describeServicesFn(ctx, input, opts...)
}

func (m *mockECSAPI) UpdateService(ctx context.Context, input *ecs.UpdateServiceInput, opts ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
	return m.updateServiceFn(ctx, input, opts...)
}

func (m *mockECSAPI) ListTasks(ctx context.Context, input *ecs.ListTasksInput, opts ...func(*ecs.Options)) (*ecs.ListTasksOutput, error) {
	return m.listTasksFn(ctx, input, opts...)
}

func (m *mockECSAPI) DescribeTasks(ctx context.Context, input *ecs.DescribeTasksInput, opts ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
	return m.describeTasksFn(ctx, input, opts...)
}

func (m *mockECSAPI) UpdateTaskProtection(ctx context.Context, input *ecs.UpdateTaskProtectionInput, opts ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error) {
	return m.updateTaskProtectionFn(ctx, input, opts...)
}

const (
	testCluster = "my-cluster"
	testService = "tfc-agent"
)

func TestGetServiceStatus(t *testing.T) {
	tests := []struct {
		name        string
		output      *ecs.DescribeServicesOutput
		err         error
		wantDesired int32
		wantRunning int32
		wantErr     bool
	}{
		{
			name: "healthy service",
			output: &ecs.DescribeServicesOutput{
				Services: []types.Service{
					{
						DesiredCount: 5,
						RunningCount: 5,
					},
				},
			},
			wantDesired: 5,
			wantRunning: 5,
		},
		{
			name: "scaling up",
			output: &ecs.DescribeServicesOutput{
				Services: []types.Service{
					{
						DesiredCount: 10,
						RunningCount: 3,
					},
				},
			},
			wantDesired: 10,
			wantRunning: 3,
		},
		{
			name: "no services found",
			output: &ecs.DescribeServicesOutput{
				Services: []types.Service{},
			},
			wantErr: true,
		},
		{
			name:    "API error",
			err:     errors.New("access denied"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Client{
				cluster: testCluster,
				service: testService,
				api: &mockECSAPI{
					describeServicesFn: func(_ context.Context, _ *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
						if tt.err != nil {
							return nil, tt.err
						}
						return tt.output, nil
					},
				},
			}

			desired, running, err := c.GetServiceStatus(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if desired != tt.wantDesired {
				t.Errorf("desired: got %d, want %d", desired, tt.wantDesired)
			}
			if running != tt.wantRunning {
				t.Errorf("running: got %d, want %d", running, tt.wantRunning)
			}
		})
	}
}

func TestSetDesiredCount(t *testing.T) {
	tests := []struct {
		name    string
		count   int32
		err     error
		wantErr bool
	}{
		{
			name:  "successful update",
			count: 5,
		},
		{
			name:    "API error",
			count:   5,
			err:     errors.New("throttling"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedInput *ecs.UpdateServiceInput
			c := &Client{
				cluster: testCluster,
				service: testService,
				api: &mockECSAPI{
					updateServiceFn: func(_ context.Context, input *ecs.UpdateServiceInput, _ ...func(*ecs.Options)) (*ecs.UpdateServiceOutput, error) {
						capturedInput = input
						if tt.err != nil {
							return nil, tt.err
						}
						return &ecs.UpdateServiceOutput{}, nil
					},
				},
			}

			err := c.SetDesiredCount(context.Background(), tt.count)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if *capturedInput.Cluster != testCluster {
				t.Errorf("cluster: got %s, want my-cluster", *capturedInput.Cluster)
			}
			if *capturedInput.Service != testService {
				t.Errorf("service: got %s, want tfc-agent", *capturedInput.Service)
			}
			if *capturedInput.DesiredCount != tt.count {
				t.Errorf("desired count: got %d, want %d", *capturedInput.DesiredCount, tt.count)
			}
		})
	}
}

func TestGetTaskIPs(t *testing.T) {
	tests := []struct {
		name         string
		listOut      *ecs.ListTasksOutput
		listErr      error
		descOut      *ecs.DescribeTasksOutput
		descErr      error
		want         []TaskInfo
		wantErr      bool
		wantDescribe bool // whether DescribeTasks should be called
	}{
		{
			name: "tasks with ENI attachments",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []string{"arn:aws:ecs:us-east-1:123:task/cluster/task1", "arn:aws:ecs:us-east-1:123:task/cluster/task2"},
			},
			descOut: &ecs.DescribeTasksOutput{
				Tasks: []types.Task{
					{
						TaskArn: aws.String("arn:aws:ecs:us-east-1:123:task/cluster/task1"),
						Attachments: []types.Attachment{
							{
								Type: aws.String("ElasticNetworkInterface"),
								Details: []types.KeyValuePair{
									{Name: aws.String("subnetId"), Value: aws.String("subnet-123")},
									{Name: aws.String("privateIPv4Address"), Value: aws.String("10.0.1.5")},
								},
							},
						},
					},
					{
						TaskArn: aws.String("arn:aws:ecs:us-east-1:123:task/cluster/task2"),
						Attachments: []types.Attachment{
							{
								Type: aws.String("ElasticNetworkInterface"),
								Details: []types.KeyValuePair{
									{Name: aws.String("privateIPv4Address"), Value: aws.String("10.0.1.6")},
								},
							},
						},
					},
				},
			},
			wantDescribe: true,
			want: []TaskInfo{
				{TaskArn: "arn:aws:ecs:us-east-1:123:task/cluster/task1", PrivateIP: "10.0.1.5"},
				{TaskArn: "arn:aws:ecs:us-east-1:123:task/cluster/task2", PrivateIP: "10.0.1.6"},
			},
		},
		{
			name: "empty task list",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []string{},
			},
			wantDescribe: false,
			want:         nil,
		},
		{
			name: "task without ENI attachment",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []string{"arn:aws:ecs:us-east-1:123:task/cluster/task1"},
			},
			descOut: &ecs.DescribeTasksOutput{
				Tasks: []types.Task{
					{
						TaskArn:     aws.String("arn:aws:ecs:us-east-1:123:task/cluster/task1"),
						Attachments: []types.Attachment{},
					},
				},
			},
			wantDescribe: true,
			want: []TaskInfo{
				{TaskArn: "arn:aws:ecs:us-east-1:123:task/cluster/task1", PrivateIP: ""},
			},
		},
		{
			name:    "ListTasks API error",
			listErr: errors.New("access denied"),
			wantErr: true,
		},
		{
			name: "DescribeTasks API error",
			listOut: &ecs.ListTasksOutput{
				TaskArns: []string{"arn:aws:ecs:us-east-1:123:task/cluster/task1"},
			},
			descErr:      errors.New("throttling"),
			wantDescribe: true,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			describeCalled := false
			c := &Client{
				cluster: testCluster,
				service: testService,
				api: &mockECSAPI{
					listTasksFn: func(_ context.Context, input *ecs.ListTasksInput, _ ...func(*ecs.Options)) (*ecs.ListTasksOutput, error) {
						if *input.Cluster != testCluster {
							t.Errorf("ListTasks cluster: got %s, want my-cluster", *input.Cluster)
						}
						if *input.ServiceName != testService {
							t.Errorf("ListTasks service: got %s, want tfc-agent", *input.ServiceName)
						}
						if tt.listErr != nil {
							return nil, tt.listErr
						}
						return tt.listOut, nil
					},
					describeTasksFn: func(_ context.Context, input *ecs.DescribeTasksInput, _ ...func(*ecs.Options)) (*ecs.DescribeTasksOutput, error) {
						describeCalled = true
						if *input.Cluster != testCluster {
							t.Errorf("DescribeTasks cluster: got %s, want my-cluster", *input.Cluster)
						}
						if tt.descErr != nil {
							return nil, tt.descErr
						}
						return tt.descOut, nil
					},
				},
			}

			got, err := c.GetTaskIPs(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantDescribe != describeCalled {
				t.Errorf("DescribeTasks called: got %v, want %v", describeCalled, tt.wantDescribe)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("task count: got %d, want %d", len(got), len(tt.want))
			}
			for i, task := range got {
				if task.TaskArn != tt.want[i].TaskArn {
					t.Errorf("task[%d].TaskArn: got %s, want %s", i, task.TaskArn, tt.want[i].TaskArn)
				}
				if task.PrivateIP != tt.want[i].PrivateIP {
					t.Errorf("task[%d].PrivateIP: got %s, want %s", i, task.PrivateIP, tt.want[i].PrivateIP)
				}
			}
		})
	}
}

func TestSetTaskProtection(t *testing.T) {
	t.Run("single batch", func(t *testing.T) {
		var calls []*ecs.UpdateTaskProtectionInput
		c := &Client{
			cluster: testCluster,
			service: testService,
			api: &mockECSAPI{
				updateTaskProtectionFn: func(_ context.Context, input *ecs.UpdateTaskProtectionInput, _ ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error) {
					calls = append(calls, input)
					return &ecs.UpdateTaskProtectionOutput{}, nil
				},
			},
		}

		arns := []string{"arn:task/1", "arn:task/2", "arn:task/3"}
		err := c.SetTaskProtection(context.Background(), arns, true, 60)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(calls) != 1 {
			t.Fatalf("API calls: got %d, want 1", len(calls))
		}
		if *calls[0].Cluster != testCluster {
			t.Errorf("cluster: got %s, want my-cluster", *calls[0].Cluster)
		}
		if len(calls[0].Tasks) != 3 {
			t.Errorf("tasks: got %d, want 3", len(calls[0].Tasks))
		}
		if !calls[0].ProtectionEnabled {
			t.Error("ProtectionEnabled: got false, want true")
		}
		if calls[0].ExpiresInMinutes == nil || *calls[0].ExpiresInMinutes != 60 {
			t.Error("ExpiresInMinutes: expected 60")
		}
	})

	t.Run("multiple batches", func(t *testing.T) {
		var calls []*ecs.UpdateTaskProtectionInput
		c := &Client{
			cluster: testCluster,
			service: testService,
			api: &mockECSAPI{
				updateTaskProtectionFn: func(_ context.Context, input *ecs.UpdateTaskProtectionInput, _ ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error) {
					calls = append(calls, input)
					return &ecs.UpdateTaskProtectionOutput{}, nil
				},
			},
		}

		arns := make([]string, 25)
		for i := range arns {
			arns[i] = "arn:task/" + string(rune('a'+i))
		}
		err := c.SetTaskProtection(context.Background(), arns, true, 30)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(calls) != 3 {
			t.Fatalf("API calls: got %d, want 3", len(calls))
		}
		if len(calls[0].Tasks) != 10 {
			t.Errorf("batch 0: got %d tasks, want 10", len(calls[0].Tasks))
		}
		if len(calls[1].Tasks) != 10 {
			t.Errorf("batch 1: got %d tasks, want 10", len(calls[1].Tasks))
		}
		if len(calls[2].Tasks) != 5 {
			t.Errorf("batch 2: got %d tasks, want 5", len(calls[2].Tasks))
		}
	})

	t.Run("empty task list", func(t *testing.T) {
		callCount := 0
		c := &Client{
			cluster: testCluster,
			service: testService,
			api: &mockECSAPI{
				updateTaskProtectionFn: func(_ context.Context, _ *ecs.UpdateTaskProtectionInput, _ ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error) {
					callCount++
					return &ecs.UpdateTaskProtectionOutput{}, nil
				},
			},
		}

		err := c.SetTaskProtection(context.Background(), nil, true, 60)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if callCount != 0 {
			t.Errorf("API calls: got %d, want 0", callCount)
		}
	})

	t.Run("API error", func(t *testing.T) {
		c := &Client{
			cluster: testCluster,
			service: testService,
			api: &mockECSAPI{
				updateTaskProtectionFn: func(_ context.Context, _ *ecs.UpdateTaskProtectionInput, _ ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error) {
					return nil, errors.New("access denied")
				},
			},
		}

		err := c.SetTaskProtection(context.Background(), []string{"arn:task/1"}, true, 60)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("disabled protection omits ExpiresInMinutes", func(t *testing.T) {
		var captured *ecs.UpdateTaskProtectionInput
		c := &Client{
			cluster: testCluster,
			service: testService,
			api: &mockECSAPI{
				updateTaskProtectionFn: func(_ context.Context, input *ecs.UpdateTaskProtectionInput, _ ...func(*ecs.Options)) (*ecs.UpdateTaskProtectionOutput, error) {
					captured = input
					return &ecs.UpdateTaskProtectionOutput{}, nil
				},
			},
		}

		err := c.SetTaskProtection(context.Background(), []string{"arn:task/1"}, false, 60)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if captured.ProtectionEnabled {
			t.Error("ProtectionEnabled: got true, want false")
		}
		if captured.ExpiresInMinutes != nil {
			t.Error("ExpiresInMinutes: expected nil when disabled")
		}
	})
}
