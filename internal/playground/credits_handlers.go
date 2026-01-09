// Package playground provides HTTP handlers for credit pricing and usage endpoints.
//
// This file implements the API endpoints for viewing pricing and usage data,
// matching the format of the real X API console endpoints.
package playground

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// HandleCreditsPricing handles GET /api/credits/pricing
// Returns the current pricing configuration for all event types and request types.
// Query parameter: ?refresh=true to force refresh from API
func HandleCreditsPricing(creditTracker *CreditTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}
		
		// Check if refresh is requested
		if r.URL.Query().Get("refresh") == "true" {
			if err := creditTracker.ReloadPricing(true); err != nil {
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to refresh pricing: %v", err), 500)
				return
			}
		}
		
		pricing := creditTracker.GetPricing()
		
		response := map[string]interface{}{
			"eventTypePricing":  pricing.EventTypePricing,
			"requestTypePricing": pricing.RequestTypePricing,
			"defaultCost":        pricing.DefaultCost,
		}
		
		WriteJSONSafe(w, http.StatusOK, response)
	}
}

// HandleAccountUsage handles GET /api/accounts/{account_id}/usage
// Returns usage data for a specific account, grouped by eventType or requestType.
// Query parameters:
//   - interval: Time interval (e.g., "30days", "7days", "90days")
//   - groupBy: Grouping type ("eventType" or "requestType")
func HandleAccountUsage(creditTracker *CreditTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}
		
		// Extract account ID from path
		path := r.URL.Path
		// Path format: /api/accounts/{account_id}/usage
		// Remove leading/trailing slashes and split
		path = strings.Trim(path, "/")
		parts := strings.Split(path, "/")
		
		// Expected: ["api", "accounts", "{account_id}", "usage"]
		if len(parts) < 4 || parts[0] != "api" || parts[1] != "accounts" || parts[3] != "usage" {
			WriteError(w, http.StatusBadRequest, "Invalid path format. Expected: /api/accounts/{account_id}/usage", 400)
			return
		}
		
		accountID := parts[2]
		if accountID == "" {
			WriteError(w, http.StatusBadRequest, "Account ID is required", 400)
			return
		}
		
		// Get query parameters
		interval := r.URL.Query().Get("interval")
		if interval == "" {
			interval = "30days" // Default to 30 days
		}
		
		groupBy := r.URL.Query().Get("groupBy")
		if groupBy == "" {
			groupBy = "eventType" // Default to eventType
		}
		
		// Validate groupBy
		if groupBy != "eventType" && groupBy != "requestType" {
			WriteError(w, http.StatusBadRequest, "groupBy must be 'eventType' or 'requestType'", 400)
			return
		}
		
		// Get usage data
		usageResponse := creditTracker.GetUsage(accountID, interval, groupBy)
		
		WriteJSONSafe(w, http.StatusOK, usageResponse)
	}
}

// HandleAccountCost handles GET /api/accounts/{account_id}/cost
// Returns cost breakdown for a specific account.
func HandleAccountCost(creditTracker *CreditTracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", 405)
			return
		}
		
		// Extract account ID from path
		path := r.URL.Path
		path = strings.Trim(path, "/")
		parts := strings.Split(path, "/")
		
		// Expected: ["api", "accounts", "{account_id}", "cost"]
		if len(parts) < 4 || parts[0] != "api" || parts[1] != "accounts" || parts[3] != "cost" {
			WriteError(w, http.StatusBadRequest, "Invalid path format. Expected: /api/accounts/{account_id}/cost", 400)
			return
		}
		
		accountID := parts[2]
		if accountID == "" {
			WriteError(w, http.StatusBadRequest, "Account ID is required", 400)
			return
		}
		
		// Get cost breakdown
		defer func() {
			if r := recover(); r != nil {
				log.Printf("Panic in GetCostBreakdown for account %s: %v", accountID, r)
				// Log stack trace
				log.Printf("Stack trace: %+v", r)
				WriteError(w, http.StatusInternalServerError, fmt.Sprintf("Internal server error: %v", r), 500)
			}
		}()
		
		if creditTracker == nil {
			WriteError(w, http.StatusInternalServerError, "Credit tracker not initialized", 500)
			return
		}
		
		costBreakdown := creditTracker.GetCostBreakdown(accountID)
		if costBreakdown == nil {
			WriteError(w, http.StatusInternalServerError, "Failed to get cost breakdown", 500)
			return
		}
		
		WriteJSONSafe(w, http.StatusOK, costBreakdown)
	}
}
