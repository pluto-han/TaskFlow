package domaintask

import (
	"errors"
)

var ErrTaskConflict = errors.New("task version conflict")

var ErrInvalidList = errors.New("invalid task list parameters")

var ErrTaskNotFound = errors.New("task not found")

var ErrInvalidTask = errors.New("invalid task")

var ErrInvalidUpdate = errors.New("invalid task update")

var ErrPlanDayChanged = errors.New("plan day changed")
