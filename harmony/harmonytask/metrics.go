package harmonytask

import (
	"context"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"

	curiobuild "github.com/filecoin-project/curio/build"
)

var (
	taskNameTag, _ = tag.NewKey("task_name")
	sourceTag, _   = tag.NewKey("source")
	versionTag, _  = tag.NewKey("version")
	pre            = "harmonytask_"

	// tasks can be short, but can extend to hours
	durationBuckets = []float64{0.5, 1, 3, 6, 10, 20, 30, 60, 120, 300, 600, 1800, 3600, 7200, 18000, 36000}
)

// TaskMeasures groups all harmonytask metrics.
var TaskMeasures = struct {
	Uptime           *stats.Int64Measure
	TasksStarted     *stats.Int64Measure
	TasksCompleted   *stats.Int64Measure
	TasksFailed      *stats.Int64Measure
	TaskDuration     promclient.Histogram
	ActiveTasks      *stats.Int64Measure
	CpuUsage         *stats.Float64Measure
	GpuUsage         *stats.Float64Measure
	RamUsage         *stats.Float64Measure
	PollerIterations *stats.Int64Measure
	AddedTasks       *stats.Int64Measure
}{
	Uptime:         stats.Int64(pre+"uptime", "Total uptime of the node in seconds.", stats.UnitSeconds),
	TasksStarted:   stats.Int64(pre+"tasks_started", "Total number of tasks started.", stats.UnitDimensionless),
	TasksCompleted: stats.Int64(pre+"tasks_completed", "Total number of tasks completed successfully.", stats.UnitDimensionless),
	TasksFailed:    stats.Int64(pre+"tasks_failed", "Total number of tasks that failed.", stats.UnitDimensionless),
	TaskDuration: promclient.NewHistogram(promclient.HistogramOpts{
		Name:    pre + "task_duration_seconds",
		Buckets: durationBuckets,
		Help:    "The histogram of task durations in seconds.",
	}),
	ActiveTasks:      stats.Int64(pre+"active_tasks", "Current number of active tasks.", stats.UnitDimensionless),
	CpuUsage:         stats.Float64(pre+"cpu_usage", "Percentage of CPU in use.", stats.UnitDimensionless),
	GpuUsage:         stats.Float64(pre+"gpu_usage", "Percentage of GPU in use.", stats.UnitDimensionless),
	RamUsage:         stats.Float64(pre+"ram_usage", "Percentage of RAM in use.", stats.UnitDimensionless),
	PollerIterations: stats.Int64(pre+"poller_iterations", "Total number of poller iterations.", stats.UnitDimensionless),
	AddedTasks:       stats.Int64(pre+"added_tasks", "Total number of tasks added.", stats.UnitDimensionless),
}

// TaskViews groups all harmonytask-related default views.
func init() {
	err := view.Register(
		&view.View{
			Measure:     TaskMeasures.Uptime,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{versionTag},
		},
		&view.View{
			Measure:     TaskMeasures.TasksStarted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{taskNameTag, sourceTag},
		},
		&view.View{
			Measure:     TaskMeasures.TasksCompleted,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{taskNameTag},
		},
		&view.View{
			Measure:     TaskMeasures.TasksFailed,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{taskNameTag},
		},
		&view.View{
			Measure:     TaskMeasures.ActiveTasks,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{taskNameTag},
		},
		&view.View{
			Measure:     TaskMeasures.CpuUsage,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{},
		},
		&view.View{
			Measure:     TaskMeasures.GpuUsage,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{},
		},
		&view.View{
			Measure:     TaskMeasures.RamUsage,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{},
		},
		&view.View{
			Measure:     TaskMeasures.PollerIterations,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{},
		},
		&view.View{
			Measure:     TaskMeasures.AddedTasks,
			Aggregation: view.Sum(),
			TagKeys:     []tag.Key{taskNameTag},
		},
	)
	if err != nil {
		panic(err)
	}

	err = promclient.Register(TaskMeasures.TaskDuration)
	if err != nil {
		panic(err)
	}

	// record uptime every 10 seconds
	go func() {
		v := curiobuild.UserVersion()
		bootTime := time.Now()

		for {
			time.Sleep(10 * time.Second)
			err := stats.RecordWithTags(context.Background(), []tag.Mutator{
				tag.Upsert(versionTag, v),
			}, TaskMeasures.Uptime.M(int64(time.Since(bootTime).Seconds())))
			if err != nil {
				log.Errorw("Could not record uptime", "error", err)
			}
		}
	}()
}
