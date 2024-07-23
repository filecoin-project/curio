/*
HarmonyCron starts tasks at a specified time & associates them with a row in a SQL table.
It supports: At(when time.Time, taskType, sqlTable string, sqlRowID int)

	which will add a task to the task engine at the specified time and associate it with the specified row.

Operation:

	The cron-task will be picked up by Cron runners, which try to avoid being sealers or provers.
	The cron-task is held until the specified time in seconds, then it completes after starting
	the task in the task engine.

Requirement: The sqlTable must have columns "id" and "task_id" to be able to update the task_id.
HarmonyCron is a regular harmonytask.TaskInterface task ran by all nodes.
*/
package harmonycron
