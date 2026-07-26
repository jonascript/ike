package store

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jonascript/ike/internal/task"
)

// Add creates a new task in the given quadrant and returns it.
func (s *Store) Add(title string, q task.Quadrant) (task.Task, error) {
	title = strings.TrimSpace(title)
	if err := task.Validate(title, q); err != nil {
		return task.Task{}, err
	}
	var created task.Task
	_, err := s.Mutate(func(d *Data) error {
		created = task.Task{
			ID:        d.NextID,
			Title:     title,
			Quadrant:  q,
			CreatedAt: time.Now().UTC(),
		}
		d.NextID++
		d.Tasks = append(d.Tasks, created)
		return nil
	})
	return created, err
}

// Complete moves a task to the archive, stamping DoneAt.
func (s *Store) Complete(id int) (task.Task, error) {
	var done task.Task
	_, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		done = d.Tasks[i]
		now := time.Now().UTC()
		done.DoneAt = &now
		d.Tasks = append(d.Tasks[:i], d.Tasks[i+1:]...)
		d.Archive = append(d.Archive, done)
		return nil
	})
	return done, err
}

// Move changes a task's quadrant.
func (s *Store) Move(id int, q task.Quadrant) (task.Task, error) {
	if !q.Valid() {
		return task.Task{}, fmt.Errorf("quadrant must be 1-4, got %d", q)
	}
	var moved task.Task
	_, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		d.Tasks[i].Quadrant = q
		moved = d.Tasks[i]
		return nil
	})
	return moved, err
}

// Rename changes a task's title.
func (s *Store) Rename(id int, title string) (task.Task, error) {
	title = strings.TrimSpace(title)
	var renamed task.Task
	_, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		if err := task.Validate(title, d.Tasks[i].Quadrant); err != nil {
			return err
		}
		d.Tasks[i].Title = title
		renamed = d.Tasks[i]
		return nil
	})
	return renamed, err
}

// Delete removes an active task permanently (it does not go to the archive).
func (s *Store) Delete(id int) (task.Task, error) {
	var deleted task.Task
	_, err := s.Mutate(func(d *Data) error {
		i, err := findTask(d, id)
		if err != nil {
			return err
		}
		deleted = d.Tasks[i]
		d.Tasks = append(d.Tasks[:i], d.Tasks[i+1:]...)
		return nil
	})
	return deleted, err
}

// List returns active tasks, optionally filtered to one quadrant (q == 0
// means all), ordered by quadrant then creation.
func (s *Store) List(q task.Quadrant) ([]task.Task, error) {
	d, err := s.Load()
	if err != nil {
		return nil, err
	}
	tasks := make([]task.Task, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		if q == 0 || t.Quadrant == q {
			tasks = append(tasks, t)
		}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Quadrant != tasks[j].Quadrant {
			return tasks[i].Quadrant < tasks[j].Quadrant
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks, nil
}

// ListArchive returns archived tasks, newest completion first.
func (s *Store) ListArchive() ([]task.Task, error) {
	d, err := s.Load()
	if err != nil {
		return nil, err
	}
	arch := make([]task.Task, len(d.Archive))
	copy(arch, d.Archive)
	sort.SliceStable(arch, func(i, j int) bool {
		ti, tj := arch[i].DoneAt, arch[j].DoneAt
		if ti != nil && tj != nil && !ti.Equal(*tj) {
			return ti.After(*tj)
		}
		return arch[i].ID > arch[j].ID
	})
	return arch, nil
}

func findTask(d *Data, id int) (int, error) {
	for i, t := range d.Tasks {
		if t.ID == id {
			return i, nil
		}
	}
	return 0, fmt.Errorf("no active task with id %d", id)
}
