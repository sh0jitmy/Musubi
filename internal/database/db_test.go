// Copyright 2026 [Copyright Holder]
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Author: [YOUR_NAME]

package database

import (
	"context"
	"testing"

	"github.com/shjtmy/go_sh0jitmy_template/ent/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestNewClient_SQLite(t *testing.T) {
	defer goleak.VerifyNone(t)

	ctx := context.Background()
	client, err := NewClient(ctx, "sqlite3", "file:db_test?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer client.Close()

	// Seed admin user
	err = SeedAdminUser(ctx, client)
	assert.NoError(t, err)

	// Verify admin exists
	exists, err := client.User.Query().Where(user.Username("admin")).Exist(ctx)
	assert.NoError(t, err)
	assert.True(t, exists)

	// Second seed should be idempotent
	err = SeedAdminUser(ctx, client)
	assert.NoError(t, err)

	// Test "sqlite" alias driver
	client2, err := NewClient(ctx, "sqlite", "file:db_test2?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	defer client2.Close()

	// Unsupported driver error
	_, err = NewClient(ctx, "unsupported_driver", "dsn")
	assert.Error(t, err)
}
