package playground

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSnowflakeID(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		wantError bool
		checkMsg  func(*testing.T, *ValidationError)
	}{
		{
			name:      "Valid 19-digit ID",
			id:        "1234567890123456789",
			wantError: false,
		},
		{
			name:      "Valid short ID",
			id:        "12345",
			wantError: false,
		},
		{
			name:      "Valid single digit",
			id:        "1",
			wantError: false,
		},
		{
			name:      "Empty ID",
			id:        "",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "id", err.Parameter)
				assert.Contains(t, err.Message, "cannot be empty")
			},
		},
		{
			name:      "ID with letters",
			id:        "123abc456",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "id", err.Parameter)
				assert.Contains(t, err.Message, "not valid")
			},
		},
		{
			name:      "ID with special characters",
			id:        "123-456",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "id", err.Parameter)
				assert.Contains(t, err.Message, "not valid")
			},
		},
		{
			name:      "ID too long (20 digits)",
			id:        "12345678901234567890",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "id", err.Parameter)
				assert.Contains(t, err.Message, "not valid")
			},
		},
		{
			name:      "Negative number string",
			id:        "-123",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "id", err.Parameter)
				assert.Contains(t, err.Message, "not valid")
			},
		},
		{
			name:      "Zero",
			id:        "0",
			wantError: false,
		},
		{
			name:      "Max int64 value",
			id:        "9223372036854775807",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSnowflakeID(tt.id)
			if tt.wantError {
				require.NotNil(t, err, "Expected validation error")
				if tt.checkMsg != nil {
					tt.checkMsg(t, err)
				}
			} else {
				assert.Nil(t, err, "Expected no validation error for valid ID: %s", tt.id)
			}
		})
	}
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name      string
		username  string
		wantError bool
		checkMsg  func(*testing.T, *ValidationError)
	}{
		{
			name:      "Valid username",
			username:  "testuser",
			wantError: false,
		},
		{
			name:      "Valid username with numbers",
			username:  "user123",
			wantError: false,
		},
		{
			name:      "Valid username with underscore",
			username:  "test_user",
			wantError: false,
		},
		{
			name:      "Valid username max length",
			username:  "123456789012345",
			wantError: false,
		},
		{
			name:      "Valid single character",
			username:  "a",
			wantError: false,
		},
		{
			name:      "Empty username",
			username:  "",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "username", err.Parameter)
				assert.Contains(t, err.Message, "cannot be empty")
			},
		},
		{
			name:      "Username too long",
			username:  "1234567890123456",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "username", err.Parameter)
				assert.Contains(t, err.Message, "does not match")
			},
		},
		{
			name:      "Username with hyphen",
			username:  "test-user",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "username", err.Parameter)
				assert.Contains(t, err.Message, "does not match")
			},
		},
		{
			name:      "Username with space",
			username:  "test user",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "username", err.Parameter)
				assert.Contains(t, err.Message, "does not match")
			},
		},
		{
			name:      "Username with special characters",
			username:  "test@user",
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "username", err.Parameter)
				assert.Contains(t, err.Message, "does not match")
			},
		},
		{
			name:      "Username with uppercase",
			username:  "TestUser",
			wantError: false,
		},
		{
			name:      "Username all numbers",
			username:  "123456789",
			wantError: false,
		},
		{
			name:      "Username all underscores",
			username:  "_________",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUsername(tt.username)
			if tt.wantError {
				require.NotNil(t, err, "Expected validation error")
				if tt.checkMsg != nil {
					tt.checkMsg(t, err)
				}
			} else {
				assert.Nil(t, err, "Expected no validation error for valid username: %s", tt.username)
			}
		})
	}
}

func TestExtractPathParameter(t *testing.T) {
	tests := []struct {
		name       string
		requestPath string
		paramName   string
		specPath    string
		wantValue   string
	}{
		{
			name:        "Extract user ID from path",
			requestPath: "/2/users/12345",
			paramName:   "id",
			specPath:    "/2/users/{id}",
			wantValue:   "12345",
		},
		{
			name:        "Extract username from path",
			requestPath: "/2/users/by/username/testuser",
			paramName:   "username",
			specPath:    "/2/users/by/username/{username}",
			wantValue:   "testuser",
		},
		{
			name:        "Extract tweet ID",
			requestPath: "/2/tweets/67890",
			paramName:   "id",
			specPath:    "/2/tweets/{id}",
			wantValue:   "67890",
		},
		{
			name:        "Path with query parameters",
			requestPath: "/2/users/12345?user.fields=id,name",
			paramName:   "id",
			specPath:    "/2/users/{id}",
			wantValue:   "12345",
		},
		{
			name:        "Multiple path parameters",
			requestPath: "/2/users/12345/following/67890",
			paramName:   "target_user_id",
			specPath:    "/2/users/{id}/following/{target_user_id}",
			wantValue:   "67890",
		},
		{
			name:        "Parameter not found",
			requestPath: "/2/users/12345",
			paramName:   "username",
			specPath:    "/2/users/{id}",
			wantValue:   "",
		},
		{
			name:        "Path length mismatch",
			requestPath: "/2/users/12345",
			paramName:   "id",
			specPath:    "/2/users/{id}/extra",
			wantValue:   "",
		},
		{
			name:        "Empty request path",
			requestPath: "",
			paramName:   "id",
			specPath:    "/2/users/{id}",
			wantValue:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPathParameter(tt.requestPath, tt.paramName, tt.specPath)
			assert.Equal(t, tt.wantValue, result, "extractPathParameter(%q, %q, %q) = %q, want %q",
				tt.requestPath, tt.paramName, tt.specPath, result, tt.wantValue)
		})
	}
}

func TestValidateParameterValue_String(t *testing.T) {
	spec := &OpenAPISpec{
		Components: make(map[string]interface{}),
	}

	tests := []struct {
		name      string
		value     string
		param     Parameter
		wantError bool
		checkMsg  func(*testing.T, *ValidationError)
	}{
		{
			name:  "Valid string without constraints",
			value: "test",
			param: Parameter{
				Name: "query",
				Schema: map[string]interface{}{
					"type": "string",
				},
			},
			wantError: false,
		},
		{
			name:  "String too short",
			value: "ab",
			param: Parameter{
				Name: "query",
				Schema: map[string]interface{}{
					"type":      "string",
					"minLength": 3.0,
				},
			},
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "query", err.Parameter)
				assert.Contains(t, err.Message, "at least 3")
			},
		},
		{
			name:  "String too long",
			value: "abcdefghijklmnopqrstuvwxyz",
			param: Parameter{
				Name: "query",
				Schema: map[string]interface{}{
					"type":      "string",
					"maxLength": 10.0,
				},
			},
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "query", err.Parameter)
				assert.Contains(t, err.Message, "at most 10")
			},
		},
		{
			name:  "String with valid enum",
			value: "public",
			param: Parameter{
				Name: "visibility",
				Schema: map[string]interface{}{
					"type": "string",
					"enum": []interface{}{"public", "private", "unlisted"},
				},
			},
			wantError: false,
		},
		{
			name:  "String with invalid enum",
			value: "invalid",
			param: Parameter{
				Name: "visibility",
				Schema: map[string]interface{}{
					"type": "string",
					"enum": []interface{}{"public", "private", "unlisted"},
				},
			},
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "visibility", err.Parameter)
				assert.Contains(t, err.Message, "must be one of")
			},
		},
		{
			name:  "String at minLength boundary",
			value: "abc",
			param: Parameter{
				Name: "query",
				Schema: map[string]interface{}{
					"type":      "string",
					"minLength": 3.0,
				},
			},
			wantError: false,
		},
		{
			name:  "String at maxLength boundary",
			value: "abcdefghij",
			param: Parameter{
				Name: "query",
				Schema: map[string]interface{}{
					"type":      "string",
					"maxLength": 10.0,
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateParameterValue(tt.value, tt.param, spec)
			if tt.wantError {
				require.NotNil(t, err, "Expected validation error")
				if tt.checkMsg != nil {
					tt.checkMsg(t, err)
				}
			} else {
				assert.Nil(t, err, "Expected no validation error")
			}
		})
	}
}

func TestValidateParameterValue_Integer(t *testing.T) {
	spec := &OpenAPISpec{
		Components: make(map[string]interface{}),
	}

	tests := []struct {
		name      string
		value     string
		param     Parameter
		wantError bool
		checkMsg  func(*testing.T, *ValidationError)
	}{
		{
			name:  "Valid integer",
			value: "100",
			param: Parameter{
				Name: "max_results",
				Schema: map[string]interface{}{
					"type": "integer",
				},
			},
			wantError: false,
		},
		{
			name:  "Integer below minimum",
			value: "5",
			param: Parameter{
				Name: "max_results",
				Schema: map[string]interface{}{
					"type":    "integer",
					"minimum": 10.0,
				},
			},
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "max_results", err.Parameter)
				assert.Contains(t, err.Message, "at least")
			},
		},
		{
			name:  "Integer above maximum",
			value: "200",
			param: Parameter{
				Name: "max_results",
				Schema: map[string]interface{}{
					"type":    "integer",
					"maximum": 100.0,
				},
			},
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "max_results", err.Parameter)
				assert.Contains(t, err.Message, "at most")
			},
		},
		{
			name:  "Integer at minimum boundary",
			value: "10",
			param: Parameter{
				Name: "max_results",
				Schema: map[string]interface{}{
					"type":    "integer",
					"minimum": 10.0,
				},
			},
			wantError: false,
		},
		{
			name:  "Integer at maximum boundary",
			value: "100",
			param: Parameter{
				Name: "max_results",
				Schema: map[string]interface{}{
					"type":    "integer",
					"maximum": 100.0,
				},
			},
			wantError: false,
		},
		{
			name:  "Invalid integer format",
			value: "not-a-number",
			param: Parameter{
				Name: "max_results",
				Schema: map[string]interface{}{
					"type": "integer",
				},
			},
			wantError: true,
			checkMsg: func(t *testing.T, err *ValidationError) {
				assert.Equal(t, "max_results", err.Parameter)
				assert.Contains(t, err.Message, "must be a")
			},
		},
		{
			name:  "Negative integer within range",
			value: "-10",
			param: Parameter{
				Name: "max_results",
				Schema: map[string]interface{}{
					"type":    "integer",
					"minimum": -100.0,
					"maximum": 100.0,
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateParameterValue(tt.value, tt.param, spec)
			if tt.wantError {
				require.NotNil(t, err, "Expected validation error")
				if tt.checkMsg != nil {
					tt.checkMsg(t, err)
				}
			} else {
				assert.Nil(t, err, "Expected no validation error")
			}
		})
	}
}

func TestValidateParameterValue_Boolean(t *testing.T) {
	spec := &OpenAPISpec{
		Components: make(map[string]interface{}),
	}

	tests := []struct {
		name      string
		value     string
		param     Parameter
		wantError bool
	}{
		{
			name:  "Valid boolean true",
			value: "true",
			param: Parameter{
				Name: "tweet_mode",
				Schema: map[string]interface{}{
					"type": "boolean",
				},
			},
			wantError: false,
		},
		{
			name:  "Valid boolean false",
			value: "false",
			param: Parameter{
				Name: "tweet_mode",
				Schema: map[string]interface{}{
					"type": "boolean",
				},
			},
			wantError: false,
		},
		{
			name:  "Invalid boolean value",
			value: "yes",
			param: Parameter{
				Name: "tweet_mode",
				Schema: map[string]interface{}{
					"type": "boolean",
				},
			},
			wantError: true,
		},
		{
			name:  "Invalid boolean value numeric",
			value: "1",
			param: Parameter{
				Name: "tweet_mode",
				Schema: map[string]interface{}{
					"type": "boolean",
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateParameterValue(tt.value, tt.param, spec)
			if tt.wantError {
				assert.NotNil(t, err, "Expected validation error")
				assert.Contains(t, err.Message, "must be 'true' or 'false'")
			} else {
				assert.Nil(t, err, "Expected no validation error")
			}
		})
	}
}

func TestValidateRequest_PathParameters(t *testing.T) {
	spec := &OpenAPISpec{
		Components: make(map[string]interface{}),
	}

	tests := []struct {
		name       string
		method     string
		path       string
		operation  *Operation
		specPath   string
		wantErrors int
	}{
		{
			name:   "Valid path parameter",
			method: "GET",
			path:   "/2/users/12345",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "id",
						In:       "path",
						Required: true,
						Schema: map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
			specPath:   "/2/users/{id}",
			wantErrors: 0,
		},
		{
			name:   "Missing required path parameter",
			method: "GET",
			path:   "/2/users",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "id",
						In:       "path",
						Required: true,
						Schema: map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
			specPath:   "/2/users/{id}",
			wantErrors: 1,
		},
		{
			name:   "Invalid path parameter value",
			method: "GET",
			path:   "/2/users/abc",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "id",
						In:       "path",
						Required: true,
						Schema: map[string]interface{}{
							"type":      "string",
							"minLength": 5.0,
						},
					},
				},
			},
			specPath:   "/2/users/{id}",
			wantErrors: 1,
		},
		{
			name:   "Optional path parameter missing",
			method: "GET",
			path:   "/2/users",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "id",
						In:       "path",
						Required: false,
						Schema: map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
			specPath:   "/2/users/{id}",
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			errors := ValidateRequest(req, tt.operation, tt.specPath, spec)
			assert.Len(t, errors, tt.wantErrors, "Expected %d validation errors, got %d", tt.wantErrors, len(errors))
		})
	}
}

func TestValidateRequest_QueryParameters(t *testing.T) {
	spec := &OpenAPISpec{
		Components: make(map[string]interface{}),
	}

	tests := []struct {
		name       string
		method     string
		path       string
		query      string
		operation  *Operation
		specPath   string
		wantErrors int
	}{
		{
			name:   "Valid query parameter",
			method: "GET",
			path:   "/2/users/me",
			query:  "max_results=10",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "max_results",
						In:       "query",
						Required: false,
						Schema: map[string]interface{}{
							"type":    "integer",
							"minimum": 10.0,
							"maximum": 100.0,
						},
					},
				},
			},
			specPath:   "/2/users/me",
			wantErrors: 0,
		},
		{
			name:   "Missing required query parameter",
			method: "GET",
			path:   "/2/users/me",
			query:  "",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "max_results",
						In:       "query",
						Required: true,
						Schema: map[string]interface{}{
							"type": "integer",
						},
					},
				},
			},
			specPath:   "/2/users/me",
			wantErrors: 1,
		},
		{
			name:   "Query parameter below minimum",
			method: "GET",
			path:   "/2/users/me",
			query:  "max_results=5",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "max_results",
						In:       "query",
						Required: false,
						Schema: map[string]interface{}{
							"type":    "integer",
							"minimum": 10.0,
						},
					},
				},
			},
			specPath:   "/2/users/me",
			wantErrors: 1,
		},
		{
			name:   "Query parameter above maximum",
			method: "GET",
			path:   "/2/users/me",
			query:  "max_results=200",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "max_results",
						In:       "query",
						Required: false,
						Schema: map[string]interface{}{
							"type":    "integer",
							"maximum": 100.0,
						},
					},
				},
			},
			specPath:   "/2/users/me",
			wantErrors: 1,
		},
		{
			name:   "Multiple query parameters",
			method: "GET",
			path:   "/2/users/me",
			query:  "max_results=50&pagination_token=abc",
			operation: &Operation{
				Parameters: []Parameter{
					{
						Name:     "max_results",
						In:       "query",
						Required: false,
						Schema: map[string]interface{}{
							"type":    "integer",
							"minimum": 10.0,
							"maximum": 100.0,
						},
					},
					{
						Name:     "pagination_token",
						In:       "query",
						Required: false,
						Schema: map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
			specPath:   "/2/users/me",
			wantErrors: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fullURL := tt.path
			if tt.query != "" {
				fullURL += "?" + tt.query
			}
			req, err := http.NewRequest(tt.method, fullURL, nil)
			require.NoError(t, err)

			errors := ValidateRequest(req, tt.operation, tt.specPath, spec)
			assert.Len(t, errors, tt.wantErrors, "Expected %d validation errors, got %d", tt.wantErrors, len(errors))
		})
	}
}

func TestFormatValidationErrors(t *testing.T) {
	tests := []struct {
		name     string
		errors   []*ValidationError
		checkRes func(*testing.T, map[string]interface{})
	}{
		{
			name:   "Empty errors",
			errors: []*ValidationError{},
			checkRes: func(t *testing.T, res map[string]interface{}) {
				assert.Nil(t, res)
			},
		},
		{
			name: "Single error",
			errors: []*ValidationError{
				{
					Parameter: "max_results",
					Message:   "value must be at least 10",
					Value:     "5",
				},
			},
			checkRes: func(t *testing.T, res map[string]interface{}) {
				require.NotNil(t, res)
				assert.Equal(t, "Invalid Request", res["title"])
				assert.Equal(t, "One or more parameters to your request was invalid.", res["detail"])
				assert.Equal(t, "https://api.twitter.com/2/problems/invalid-request", res["type"])

				errors, ok := res["errors"].([]map[string]interface{})
				require.True(t, ok)
				assert.Len(t, errors, 1)
				assert.Equal(t, "value must be at least 10", errors[0]["message"])
			},
		},
		{
			name: "Multiple errors",
			errors: []*ValidationError{
				{
					Parameter: "max_results",
					Message:   "value must be at least 10",
					Value:     "5",
				},
				{
					Parameter: "username",
					Message:   "Username cannot be empty",
					Value:     "",
				},
			},
			checkRes: func(t *testing.T, res map[string]interface{}) {
				require.NotNil(t, res)
				errors, ok := res["errors"].([]map[string]interface{})
				require.True(t, ok)
				assert.Len(t, errors, 2)
			},
		},
		{
			name: "Multiple errors for same parameter",
			errors: []*ValidationError{
				{
					Parameter: "max_results",
					Message:   "value must be at least 10",
					Value:     "5",
				},
				{
					Parameter: "max_results",
					Message:   "value must be at most 100",
					Value:     "200",
				},
			},
			checkRes: func(t *testing.T, res map[string]interface{}) {
				require.NotNil(t, res)
				errors, ok := res["errors"].([]map[string]interface{})
				require.True(t, ok)
				assert.Len(t, errors, 2)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatValidationErrors(tt.errors)
			tt.checkRes(t, result)
		})
	}
}

func TestFormatSingleValidationError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ValidationError
		checkRes func(*testing.T, map[string]interface{})
	}{
		{
			name: "Nil error",
			err:  nil,
			checkRes: func(t *testing.T, res map[string]interface{}) {
				assert.Nil(t, res)
			},
		},
		{
			name: "Valid error",
			err: &ValidationError{
				Parameter: "max_results",
				Message:   "value must be at least 10",
				Value:     "5",
			},
			checkRes: func(t *testing.T, res map[string]interface{}) {
				require.NotNil(t, res)
				assert.Equal(t, "Invalid Request", res["title"])
				assert.Equal(t, "One or more parameters to your request was invalid.", res["detail"])
				assert.Equal(t, "https://api.twitter.com/2/problems/invalid-request", res["type"])

				errors, ok := res["errors"].([]map[string]interface{})
				require.True(t, ok)
				assert.Len(t, errors, 1)
				assert.Equal(t, "value must be at least 10", errors[0]["message"])

				params, ok := errors[0]["parameters"].(map[string]interface{})
				require.True(t, ok)
				values, ok := params["max_results"].([]string)
				require.True(t, ok)
				assert.Contains(t, values, "5")
			},
		},
		{
			name: "Error with nil value",
			err: &ValidationError{
				Parameter: "id",
				Message:   "required path parameter is missing",
				Value:     nil,
			},
			checkRes: func(t *testing.T, res map[string]interface{}) {
				require.NotNil(t, res)
				errors, ok := res["errors"].([]map[string]interface{})
				require.True(t, ok)
				assert.Len(t, errors, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatSingleValidationError(tt.err)
			tt.checkRes(t, result)
		})
	}
}

func TestValidateRequest_WithRef(t *testing.T) {
	// Test validation with $ref schema resolution
	spec := &OpenAPISpec{
		Components: map[string]interface{}{
			"schemas": map[string]interface{}{
				"SnowflakeID": map[string]interface{}{
					"type":      "string",
					"minLength": 1.0,
					"maxLength": 19.0,
					"pattern":   "^\\d+$",
				},
			},
		},
	}

	req, err := http.NewRequest("GET", "/2/users/12345", nil)
	require.NoError(t, err)

	operation := &Operation{
		Parameters: []Parameter{
			{
				Name:     "id",
				In:       "path",
				Required: true,
				Schema: map[string]interface{}{
					"$ref": "#/components/schemas/SnowflakeID",
				},
			},
		},
	}

	errors := ValidateRequest(req, operation, "/2/users/{id}", spec)
	assert.Len(t, errors, 0, "Expected no validation errors for valid snowflake ID")
}

func TestValidateRequest_ComplexScenario(t *testing.T) {
	spec := &OpenAPISpec{
		Components: make(map[string]interface{}),
	}

	// Test a complex scenario with multiple path and query parameters
	req, err := http.NewRequest("GET", "/2/users/12345/following/67890?max_results=50&pagination_token=abc", nil)
	require.NoError(t, err)

	operation := &Operation{
		Parameters: []Parameter{
			{
				Name:     "id",
				In:       "path",
				Required: true,
				Schema: map[string]interface{}{
					"type": "string",
				},
			},
			{
				Name:     "target_user_id",
				In:       "path",
				Required: true,
				Schema: map[string]interface{}{
					"type": "string",
				},
			},
			{
				Name:     "max_results",
				In:       "query",
				Required: false,
				Schema: map[string]interface{}{
					"type":    "integer",
					"minimum": 10.0,
					"maximum": 100.0,
				},
			},
			{
				Name:     "pagination_token",
				In:       "query",
				Required: false,
				Schema: map[string]interface{}{
					"type": "string",
				},
			},
		},
	}

	errors := ValidateRequest(req, operation, "/2/users/{id}/following/{target_user_id}", spec)
	assert.Len(t, errors, 0, "Expected no validation errors for valid complex request")
}
