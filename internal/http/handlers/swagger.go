package handlers

import "net/http"

func SwaggerUI(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>planka-api Swagger</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: "/openapi.json",
        dom_id: "#swagger-ui",
        deepLinking: true,
        persistAuthorization: true
      });
    };
  </script>
</body>
</html>`))
}

func OpenAPI(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, openAPISpec())
}

func openAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.0.3",
		"info": map[string]any{
			"title":   "planka-api",
			"version": "0.1.0",
		},
		"servers": []map[string]string{
			{"url": "http://localhost:8080"},
		},
		"components": map[string]any{
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]string{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "opaque",
				},
			},
			"schemas": map[string]any{
				"AuthRequest": map[string]any{
					"type":     "object",
					"required": []string{"email", "password"},
					"properties": map[string]any{
						"email":    stringSchema("user@example.com"),
						"password": stringSchema("password123"),
						"name":     stringSchema("User"),
					},
				},
				"TokenResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"token_type":               stringSchema("Bearer"),
						"access_token":             stringSchema("access-token"),
						"expires_in":               integerSchema(900),
						"refresh_token":            stringSchema("refresh-token"),
						"refresh_token_expires_in": integerSchema(2592000),
						"user":                     refSchema("User"),
					},
				},
				"RefreshRequest": map[string]any{
					"type":     "object",
					"required": []string{"refresh_token"},
					"properties": map[string]any{
						"refresh_token": stringSchema("refresh-token"),
					},
				},
				"User": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":         uuidSchema(),
						"email":      stringSchema("user@example.com"),
						"name":       stringSchema("User"),
						"created_at": dateTimeSchema(),
					},
				},
				"ScheduleRequest": map[string]any{
					"type":     "object",
					"required": []string{"title"},
					"properties": map[string]any{
						"title": stringSchema("Work"),
					},
				},
				"Schedule": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":         uuidSchema(),
						"title":      stringSchema("Work"),
						"created_at": dateTimeSchema(),
						"updated_at": dateTimeSchema(),
					},
				},
				"Error": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"error": stringSchema("error message"),
					},
				},
			},
		},
		"paths": pathsSpec(),
	}
}

func pathsSpec() map[string]any {
	return map[string]any{
		"/healthz": map[string]any{
			"get": map[string]any{
				"tags":      []string{"Health"},
				"summary":   "Health check",
				"responses": responseMap(response("200", "OK", objectSchema(map[string]any{"status": stringSchema("ok")}))),
			},
		},
		"/auth/register": authPath("Register user", "201"),
		"/auth/login":    authPath("Login", "200"),
		"/auth/refresh": map[string]any{
			"post": map[string]any{
				"tags":        []string{"Auth"},
				"summary":     "Refresh tokens",
				"requestBody": jsonBody(refSchema("RefreshRequest")),
				"responses": responseMap(
					response("200", "Token pair", refSchema("TokenResponse")),
					errorResponse("400"),
					errorResponse("401"),
				),
			},
		},
		"/auth/logout": map[string]any{
			"post": map[string]any{
				"tags":     []string{"Auth"},
				"summary":  "Logout",
				"security": bearerSecurity(),
				"responses": responseMap(
					noContentResponse("204", "Logged out"),
					errorResponse("400"),
				),
			},
		},
		"/auth/me": map[string]any{
			"get": map[string]any{
				"tags":     []string{"Auth"},
				"summary":  "Current user",
				"security": bearerSecurity(),
				"responses": responseMap(
					response("200", "Current user", refSchema("User")),
					errorResponse("401"),
				),
			},
		},
		"/schedules": map[string]any{
			"get": map[string]any{
				"tags":     []string{"Schedules"},
				"summary":  "List schedules",
				"security": bearerSecurity(),
				"responses": responseMap(
					response("200", "Schedules", arraySchema(refSchema("Schedule"))),
					errorResponse("401"),
				),
			},
			"post": map[string]any{
				"tags":        []string{"Schedules"},
				"summary":     "Create schedule",
				"security":    bearerSecurity(),
				"requestBody": jsonBody(refSchema("ScheduleRequest")),
				"responses": responseMap(
					response("201", "Created schedule", refSchema("Schedule")),
					errorResponse("400"),
					errorResponse("401"),
				),
			},
		},
		"/schedules/{id}": map[string]any{
			"get": map[string]any{
				"tags":       []string{"Schedules"},
				"summary":    "Get schedule",
				"security":   bearerSecurity(),
				"parameters": []any{pathUUIDParameter("id")},
				"responses": responseMap(
					response("200", "Schedule", refSchema("Schedule")),
					errorResponse("400"),
					errorResponse("401"),
					errorResponse("404"),
				),
			},
			"patch": map[string]any{
				"tags":        []string{"Schedules"},
				"summary":     "Update schedule",
				"security":    bearerSecurity(),
				"parameters":  []any{pathUUIDParameter("id")},
				"requestBody": jsonBody(refSchema("ScheduleRequest")),
				"responses": responseMap(
					response("200", "Updated schedule", refSchema("Schedule")),
					errorResponse("400"),
					errorResponse("401"),
					errorResponse("404"),
				),
			},
			"delete": map[string]any{
				"tags":       []string{"Schedules"},
				"summary":    "Delete schedule",
				"security":   bearerSecurity(),
				"parameters": []any{pathUUIDParameter("id")},
				"responses": responseMap(
					noContentResponse("204", "Deleted"),
					errorResponse("400"),
					errorResponse("401"),
					errorResponse("404"),
				),
			},
		},
	}
}

func authPath(summary, successStatus string) map[string]any {
	return map[string]any{
		"post": map[string]any{
			"tags":        []string{"Auth"},
			"summary":     summary,
			"requestBody": jsonBody(refSchema("AuthRequest")),
			"responses": responseMap(
				response(successStatus, "Token pair", refSchema("TokenResponse")),
				errorResponse("400"),
				errorResponse("401"),
				errorResponse("409"),
			),
		},
	}
}

func jsonBody(schema map[string]any) map[string]any {
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": schema,
			},
		},
	}
}

func response(status, description string, schema map[string]any) map[string]any {
	return map[string]any{
		"status":      status,
		"description": description,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema": schema,
			},
		},
	}
}

func noContentResponse(status, description string) map[string]any {
	return map[string]any{
		"status":      status,
		"description": description,
	}
}

func errorResponse(status string) map[string]any {
	return response(status, "Error", refSchema("Error"))
}

func responseMap(responses ...map[string]any) map[string]any {
	result := make(map[string]any, len(responses))
	for _, response := range responses {
		status, _ := response["status"].(string)
		delete(response, "status")
		result[status] = response
	}

	return result
}

func bearerSecurity() []map[string][]string {
	return []map[string][]string{{"bearerAuth": []string{}}}
}

func refSchema(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func objectSchema(properties map[string]any) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": properties,
	}
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": items,
	}
}

func stringSchema(example string) map[string]any {
	return map[string]any{
		"type":    "string",
		"example": example,
	}
}

func integerSchema(example int) map[string]any {
	return map[string]any{
		"type":    "integer",
		"example": example,
	}
}

func uuidSchema() map[string]any {
	return map[string]any{
		"type":    "string",
		"format":  "uuid",
		"example": "00000000-0000-0000-0000-000000000000",
	}
}

func dateTimeSchema() map[string]any {
	return map[string]any{
		"type":    "string",
		"format":  "date-time",
		"example": "2026-05-30T12:00:00Z",
	}
}

func pathUUIDParameter(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"in":       "path",
		"required": true,
		"schema":   uuidSchema(),
	}
}
