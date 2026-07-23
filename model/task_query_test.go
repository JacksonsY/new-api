package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskListQueriesReturnDatabaseErrors(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:task-query-errors?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	originalDB := DB
	DB = db
	t.Cleanup(func() {
		DB = originalDB
	})

	params := SyncTaskQueryParams{}

	_, err = TaskGetAllTasks(0, 20, params)
	require.Error(t, err)

	_, err = TaskGetAllUserTask(1, 0, 20, params)
	require.Error(t, err)

	_, err = TaskCountAllTasks(params)
	require.Error(t, err)

	_, err = TaskCountAllUserTask(1, params)
	require.Error(t, err)
}
