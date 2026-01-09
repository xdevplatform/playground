// Configuration
const API_BASE_URL = window.location.origin || 'http://localhost:8080';
let requestHistory = JSON.parse(localStorage.getItem('requestHistory') || '[]');
// Migrate old stream history to unified request history
const oldStreamHistory = JSON.parse(localStorage.getItem('streamHistory') || '[]');
if (oldStreamHistory.length > 0 && !localStorage.getItem('streamHistoryMigrated')) {
    oldStreamHistory.forEach(streamItem => {
        // Convert stream history item to request history format
        const historyItem = {
            method: streamItem.method,
            endpoint: streamItem.endpoint,
            fullUrl: streamItem.url,
            status: streamItem.status === 'ended' ? 200 : streamItem.status === 'error' ? 0 : streamItem.status === 'stopped' ? 0 : 0,
            statusText: streamItem.status === 'ended' ? 'OK' : streamItem.status,
            timestamp: streamItem.timestamp,
            requestHeaders: { 'Authorization': streamItem.auth || '' },
            requestBody: null,
            queryParams: streamItem.parameters.filter(p => !p.isPath).map(p => `${p.key}=${p.value}`),
            responseHeaders: {},
            responseBody: null,
            isStream: true,
            streamStatus: streamItem.status,
            messageCount: streamItem.messageCount || 0,
            duration: streamItem.duration || 0,
            endTime: streamItem.endTime
        };
        requestHistory.unshift(historyItem);
    });
    localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
    localStorage.setItem('streamHistoryMigrated', 'true');
}

// Initialize
document.addEventListener('DOMContentLoaded', () => {
    initializeNavigation();
    checkServerStatus();
    loadExplorer();
    loadConfig();
    loadCredits();
    loadStateInfo();
    updateRequestHistory();
    loadEndpoints();
    
    // Initialize relationship type filter visibility
    const typeSelect = document.getElementById('unifiedExplorerType');
    const relationshipTypeFilter = document.getElementById('relationshipTypeFilter');
    if (typeSelect && relationshipTypeFilter) {
        if (typeSelect.value === 'relationships') {
            relationshipTypeFilter.style.display = 'block';
        } else {
            relationshipTypeFilter.style.display = 'none';
        }
    }
    
    // Initialize toast container positioning (bottom center)
    const toastContainer = document.getElementById('toastContainer');
    if (toastContainer) {
        toastContainer.style.position = 'fixed';
        toastContainer.style.top = 'auto';
        toastContainer.style.right = 'auto';
        toastContainer.style.left = '50%';
        toastContainer.style.bottom = '20px';
        toastContainer.style.transform = 'translateX(-50%)';
        toastContainer.style.zIndex = '10000';
        toastContainer.style.display = 'flex';
        toastContainer.style.flexDirection = 'column-reverse';
        toastContainer.style.gap = '12px';
        toastContainer.style.pointerEvents = 'none';
        toastContainer.style.alignItems = 'center';
    }
    
    // Set up request command listeners
    // Use setTimeout to ensure DOM is fully ready
    setTimeout(() => {
        setupRequestCommandListeners();
    }, 100);
    
    // Auto-refresh server status every 5 seconds
    setInterval(checkServerStatus, 5000);
    
    // Initialize auth method UI
    setTimeout(() => {
        updateAuthMethod('request');
    }, 200);
});

// Endpoints cache
let endpointsCache = [];
let currentEndpointInfo = null;

// Navigation
function initializeNavigation() {
    const navItems = document.querySelectorAll('.nav-item');
    navItems.forEach(item => {
        item.addEventListener('click', (e) => {
            e.preventDefault();
            const section = item.dataset.section;
            showSection(section);
            
            navItems.forEach(ni => ni.classList.remove('active'));
            item.classList.add('active');
        });
    });
    
    // Show request builder by default
    showSection('api-builder');
}

function showSection(sectionId) {
    document.querySelectorAll('.content-section').forEach(section => {
        section.classList.remove('active');
    });
    
    const targetSection = document.getElementById(sectionId);
    if (targetSection) {
        targetSection.classList.add('active');
        
        // Load section-specific data
        if (sectionId === 'explorer') {
            loadExplorer();
        } else if (sectionId === 'config') {
            loadConfig();
        } else if (sectionId === 'credits') {
            loadCredits();
        }
    }
}

// Server Status
let serverHealthData = null;
let serverHealthTooltipVisible = false;

async function checkServerStatus() {
    try {
        const response = await fetch(`${API_BASE_URL}/health`);
        const statusEl = document.getElementById('serverStatus');
        const dotEl = statusEl.querySelector('.status-dot');
        const textEl = statusEl.querySelector('span:last-child');
        
        if (response.ok) {
            const data = await response.json();
            serverHealthData = data; // Store health data
            dotEl.className = 'status-dot online';
            textEl.textContent = 'Online';
            
            // Update tooltip if visible
            if (serverHealthTooltipVisible) {
                updateServerHealthTooltip(data);
            }
        } else {
            throw new Error('Server not responding');
        }
    } catch (error) {
        const statusEl = document.getElementById('serverStatus');
        const dotEl = statusEl.querySelector('.status-dot');
        const textEl = statusEl.querySelector('span:last-child');
        dotEl.className = 'status-dot offline';
        textEl.textContent = 'Offline';
        serverHealthData = null;
        
        // Update tooltip if visible
        if (serverHealthTooltipVisible) {
            const tooltip = document.getElementById('serverHealthTooltip');
            if (tooltip) {
                tooltip.innerHTML = '<div style="color: var(--danger);">Server is offline</div>';
            }
        }
    }
}

function showServerHealthDetails() {
    const tooltip = document.getElementById('serverHealthTooltip');
    if (!tooltip) return;
    
    serverHealthTooltipVisible = true;
    tooltip.style.display = 'block';
    
    if (serverHealthData) {
        updateServerHealthTooltip(serverHealthData);
    } else {
        // Fetch health data if not cached
        loadServerHealthDetails();
    }
}

function hideServerHealthDetails() {
    // Only hide on mouse leave if not clicked to keep open
    if (!serverHealthTooltipVisible) return;
    
    const tooltip = document.getElementById('serverHealthTooltip');
    if (tooltip && !tooltip.dataset.pinned) {
        tooltip.style.display = 'none';
        serverHealthTooltipVisible = false;
    }
}

function toggleServerHealthDetails() {
    const tooltip = document.getElementById('serverHealthTooltip');
    if (!tooltip) return;
    
    if (tooltip.style.display === 'none' || !tooltip.style.display) {
        serverHealthTooltipVisible = true;
        tooltip.style.display = 'block';
        tooltip.dataset.pinned = 'true';
        
        if (serverHealthData) {
            updateServerHealthTooltip(serverHealthData);
        } else {
            loadServerHealthDetails();
        }
    } else {
        tooltip.style.display = 'none';
        delete tooltip.dataset.pinned;
        serverHealthTooltipVisible = false;
    }
}

async function loadServerHealthDetails() {
    const tooltip = document.getElementById('serverHealthTooltip');
    if (!tooltip) return;
    
    try {
        const response = await fetch(`${API_BASE_URL}/health`);
        if (response.ok) {
            const data = await response.json();
            serverHealthData = data;
            updateServerHealthTooltip(data);
        } else {
            throw new Error('Failed to load health');
        }
    } catch (error) {
        tooltip.innerHTML = `<div style="color: var(--danger);">Failed to load health details: ${error.message}</div>`;
    }
}

function updateServerHealthTooltip(data) {
    const tooltip = document.getElementById('serverHealthTooltip');
    if (!tooltip) return;
    
    const successRate = data.stats.requests_total > 0 
        ? Math.round((data.stats.requests_success / data.stats.requests_total) * 100) 
        : 0;
    
    tooltip.innerHTML = `
        <div style="padding: 4px 0; border-bottom: 1px solid var(--border); margin-bottom: 8px;">
            <div style="font-size: 11px; color: var(--text-muted); margin-bottom: 2px;">Status</div>
            <div style="font-size: 14px; font-weight: 600; color: var(--success);">${data.status.toUpperCase()}</div>
        </div>
        <div style="padding: 4px 0; border-bottom: 1px solid var(--border); margin-bottom: 8px;">
            <div style="font-size: 11px; color: var(--text-muted); margin-bottom: 2px;">Uptime</div>
            <div style="font-size: 14px; font-weight: 600;">${formatUptime(data.uptime_seconds)}</div>
        </div>
        <div style="padding: 4px 0; border-bottom: 1px solid var(--border); margin-bottom: 8px;">
            <div style="font-size: 11px; color: var(--text-muted); margin-bottom: 2px;">Total Requests</div>
            <div style="font-size: 14px; font-weight: 600;">${data.stats.requests_total || 0}</div>
        </div>
        <div style="padding: 4px 0; border-bottom: 1px solid var(--border); margin-bottom: 8px;">
            <div style="font-size: 11px; color: var(--text-muted); margin-bottom: 2px;">Success Rate</div>
            <div style="font-size: 14px; font-weight: 600;">${successRate}%</div>
        </div>
        <div style="padding: 4px 0;">
            <div style="font-size: 11px; color: var(--text-muted); margin-bottom: 2px;">Avg Response Time</div>
            <div style="font-size: 14px; font-weight: 600;">${(data.stats.response_time_avg_ms || 0).toFixed(2)}ms</div>
        </div>
    `;
}

// Explorer - unified view combining dashboard and explorer
async function loadExplorer() {
    await Promise.all([
        loadStats(),
        loadStateInfo()
    ]);
    
        // Load explorer data with default type (users)
        const typeSelect = document.getElementById('unifiedExplorerType');
        if (typeSelect) {
            // Ensure users is selected by default
            if (!typeSelect.value) {
                typeSelect.value = 'users';
            }
            loadExplorerData();
        }
}

function refreshExplorer() {
    loadExplorer();
}

// Health status is now shown in tooltip on server status indicator

let allEndpoints = [];
let filteredEndpoints = [];
let showAllEndpoints = false;

async function loadEndpointsForRateLimits() {
    try {
        const [rateLimitsResponse, endpointsResponse] = await Promise.all([
            fetch(`${API_BASE_URL}/rate-limits`),
            fetch(`${API_BASE_URL}/endpoints`).catch(() => null) // May fail if spec not loaded
        ]);
        
        const rateLimitsData = await rateLimitsResponse.json();
        let allEndpoints = [];
        
        if (endpointsResponse && endpointsResponse.ok) {
            const endpointsData = await endpointsResponse.json();
            allEndpoints = endpointsData.endpoints || [];
        }
        
        // Store for use in endpoint editor
        window.rateLimitEndpoints = allEndpoints;
        window.rateLimitDefaults = rateLimitsData.endpoints || [];
        window.rateLimitOverrides = currentConfig?.rate_limit?.endpoint_overrides || {};
        
        // Create a map of default limits for quick lookup
        window.rateLimitDefaultsMap = {};
        (rateLimitsData.endpoints || []).forEach(ep => {
            const key = ep.method ? `${ep.method}:${ep.endpoint}` : ep.endpoint;
            window.rateLimitDefaultsMap[key] = { limit: ep.limit, window_sec: ep.window_sec, source: 'default' };
        });
        
        return true;
    } catch (error) {
        console.error('Failed to load endpoints for rate limits:', error);
        return false;
    }
}

async function loadStats() {
    try {
        // Get state export to count items
        const response = await fetch(`${API_BASE_URL}/state/export`);
        const data = await response.json();
        
        const userCount = Object.keys(data.users || {}).length;
        const tweetCount = Object.keys(data.tweets || {}).length;
        const listCount = Object.keys(data.lists || {}).length;
        const mediaCount = Object.keys(data.media || {}).length;
        const spaceCount = Object.keys(data.spaces || {}).length;
        const communityCount = Object.keys(data.communities || {}).length;
        const dmCount = Object.keys(data.dm_conversations || {}).length;
        const streamRulesCount = Object.keys(data.search_stream_rules || {}).length;
        
        // Update stat cards (if they exist)
        const statUsers = document.getElementById('statUsers');
        const statTweets = document.getElementById('statTweets');
        const statLists = document.getElementById('statLists');
        const statMedia = document.getElementById('statMedia');
        
        if (statUsers) statUsers.textContent = userCount;
        if (statTweets) statTweets.textContent = tweetCount;
        if (statLists) statLists.textContent = listCount;
        if (statMedia) statMedia.textContent = mediaCount;
        
        // Quick stats bar elements removed - stats now displayed in stateStatsGrid
        
        // Update stat labels
        const tweetsStat = document.querySelector('.stat-icon.tweets')?.closest('.stat-card');
        if (tweetsStat) {
            const label = tweetsStat.querySelector('.stat-content p');
            if (label) label.textContent = 'Posts';
        }
    } catch (error) {
        console.error('Failed to load stats:', error);
    }
}

function refreshDashboard() {
    refreshExplorer();
}

function formatUptime(seconds) {
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
}

// Data Explorer
let explorerDataCache = {
    items: [],
    type: null,
    title: '',
    userMap: {},
    dmEventsMap: {}
};

let explorerPagination = {
    currentPage: 1,
    itemsPerPage: 50,
    totalItems: 0
};

let explorerViewMode = 'formatted'; // 'formatted' or 'json'

// Deduplicate items by ID to prevent showing duplicates
function deduplicateById(items) {
    if (!Array.isArray(items)) {
        return [];
    }
    
    const seen = new Map();
    const unique = [];
    
    for (const item of items) {
        if (!item || !item.id) {
            continue; // Skip items without IDs
        }
        
        const id = String(item.id); // Normalize ID to string
        if (!seen.has(id)) {
            seen.set(id, true);
            unique.push(item);
        }
    }
    
    return unique;
}

function switchExplorerTab(tabName) {
    // No-op: tabs removed, all content shown in main view
}

// Load detailed state info for state management tab
async function loadDetailedStateInfo() {
    try {
        const response = await fetch(`${API_BASE_URL}/state/export`);
        const data = await response.json();
        
        const statsGrid = document.getElementById('stateStatsGridDetailed');
        if (!statsGrid) return;
        
        // Ensure the grid has the proper CSS class
        if (!statsGrid.classList.contains('state-stats-grid')) {
            statsGrid.classList.add('state-stats-grid');
        }
        
        const userCount = Object.keys(data.users || {}).length;
        const tweetCount = Object.keys(data.tweets || {}).length;
        const listCount = Object.keys(data.lists || {}).length;
        const spaceCount = Object.keys(data.spaces || {}).length;
        const mediaCount = Object.keys(data.media || {}).length;
        const dmCount = Object.keys(data.dm_conversations || {}).length;
        const dmEventCount = Object.keys(data.dm_events || {}).length;
        const streamRulesCount = Object.keys(data.search_stream_rules || {}).length;
        const newsCount = Object.keys(data.news || {}).length;
        
        statsGrid.innerHTML = `
            <div class="state-stat-item">
                <div class="state-stat-label">Users</div>
                <div class="state-stat-value">${userCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Posts</div>
                <div class="state-stat-value">${tweetCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Lists</div>
                <div class="state-stat-value">${listCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Spaces</div>
                <div class="state-stat-value">${spaceCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Media</div>
                <div class="state-stat-value">${mediaCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">News</div>
                <div class="state-stat-value">${newsCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">DM Conversations</div>
                <div class="state-stat-value">${dmCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">DM Messages</div>
                <div class="state-stat-value">${dmEventCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Stream Rules</div>
                <div class="state-stat-value">${streamRulesCount}</div>
            </div>
        `;
        
        statsGrid.innerHTML = statsHtml;
        
        // Update "Last Updated" timestamp under State Actions button
        const lastUpdatedElement = document.getElementById('stateLastUpdated');
        if (lastUpdatedElement) {
            lastUpdatedElement.textContent = `Last updated: ${formatDate(data.exported_at)}`;
        }
    } catch (error) {
        const statsGrid = document.getElementById('stateStatsGridDetailed');
        if (statsGrid) {
            statsGrid.innerHTML = 
                `<div style="color: var(--danger); grid-column: 1 / -1;">Failed to load state info: ${error.message}</div>`;
        }
    }
}

// Handle unified search
function handleUnifiedSearch() {
    const searchInput = document.getElementById('unifiedExplorerSearch');
    const typeSelect = document.getElementById('unifiedExplorerType');
    const relationshipTypeFilter = document.getElementById('relationshipTypeFilter');
    const searchQuery = searchInput?.value.trim().toLowerCase() || '';
    const typeFilter = typeSelect?.value || 'users';
    
    // Update search operators tooltip
    updateSearchOperatorsTooltip(typeFilter);
    
    // Show/hide relationship type filter based on selected type
    if (relationshipTypeFilter) {
        if (typeFilter === 'relationships') {
            relationshipTypeFilter.style.display = 'block';
        } else {
            relationshipTypeFilter.style.display = 'none';
        }
    }
    
    // Update placeholder based on type
    if (searchInput) {
        if (typeFilter === 'relationships') {
            searchInput.placeholder = 'Search by username, user ID, post ID, list ID...';
        } else {
            searchInput.placeholder = 'Search users, posts, lists, spaces, communities, media, DMs, stream rules, relationships...';
        }
    }
    
    // Sync with old search input if it exists
    const oldSearchInput = document.getElementById('explorerSearch');
    if (oldSearchInput) {
        oldSearchInput.value = searchInput.value;
    }
    
    // Update the old explorer controls to match
    const oldTypeSelect = document.getElementById('explorerType');
    if (oldTypeSelect) {
        oldTypeSelect.value = typeFilter;
    }
    
    // Always load data when type changes, or when searching
    if (searchQuery || typeFilter) {
        loadExplorerDataWithSearch(searchQuery, typeFilter);
    } else {
        // Load default data for selected type
        loadExplorerData();
    }
}

// Load explorer data with search
async function loadExplorerDataWithSearch(searchQuery, typeFilter) {
    const type = typeFilter || 'users';
    const content = document.getElementById('explorerContent');
    content.innerHTML = '<div class="loading">Loading data...</div>';
    
    try {
        const response = await fetch(`${API_BASE_URL}/state/export`);
        const data = await response.json();
        
        let items = [];
        let userMap = {};
        
        // Handle relationships specially
        if (type === 'relationships') {
            const relationshipTypeFilter = document.getElementById('relationshipTypeFilter')?.value || 'all';
            items = extractRelationships(data, relationshipTypeFilter);
            userMap = data.users || {};
            
            // Enhance all items with user/post/list info (before filtering)
            // This ensures filterExplorerData can properly filter and paginate
            items = items.map(item => {
                const user = userMap[item.user_id] || {};
                const targetUser = userMap[item.target_user_id] || {};
                const targetPost = data.tweets?.[item.target_tweet_id] || {};
                const targetList = data.lists?.[item.target_list_id] || {};
                
                return {
                    ...item,
                    _user: {
                        id: user.id,
                        name: user.name || 'Unknown User',
                        username: user.username || 'unknown',
                        profile_image_url: user.profile_image_url || ''
                    },
                    _target_user: targetUser.id ? {
                        id: targetUser.id,
                        name: targetUser.name || 'Unknown User',
                        username: targetUser.username || 'unknown',
                        profile_image_url: targetUser.profile_image_url || ''
                    } : null,
                    _target_tweet: targetPost.id ? {
                        id: targetPost.id,
                        text: targetPost.text || '',
                        author_id: targetPost.author_id
                    } : null,
                    _target_list: targetList.id ? {
                        id: targetList.id,
                        name: targetList.name || '',
                        description: targetList.description || ''
                    } : null
                };
            });
            
            // Store unfiltered items in cache - let filterExplorerData handle filtering and pagination
            explorerDataCache = {
                items: items,
                type: 'relationships',
                title: 'Relationships',
                userMap: userMap,
                tweetsMap: data.tweets || {},
                listsMap: data.lists || {}
            };
            
            explorerPagination.currentPage = 1;
            filterExplorerData();
            return;
        }
        
        // Single type search (no "all" option)
        // Handle search_stream_rules key mapping
        const dataKey = type === 'search_stream_rules' ? 'search_stream_rules' : type;
        items = deduplicateById(Object.values(data[dataKey] || {}));
        items = items.map(item => ({ ...item, _type: type }));
        
        // Build user map for tweet and DM rendering
        if ((type === 'tweets' || type === 'dm_conversations') && data.users) {
            userMap = data.users;
        }
        
        // Build DM events map for conversation rendering
        let dmEventsMap = {};
        if (type === 'dm_conversations' && data.dm_events) {
            // Group DM events by conversation ID
            Object.values(data.dm_events).forEach(event => {
                if (!dmEventsMap[event.dm_conversation_id]) {
                    dmEventsMap[event.dm_conversation_id] = [];
                }
                dmEventsMap[event.dm_conversation_id].push(event);
            });
            // Sort messages within each conversation by created_at
            Object.keys(dmEventsMap).forEach(convID => {
                dmEventsMap[convID].sort((a, b) => {
                    const dateA = new Date(a.created_at || 0);
                    const dateB = new Date(b.created_at || 0);
                    return dateA - dateB; // Oldest first (chronological order)
                });
            });
        }
        
        // Apply search filter with support for operators
        if (searchQuery) {
            items = items.filter(item => {
                return matchesSearchWithOperators(item, searchQuery, type, data);
            });
        }
        
        // For posts, enhance with author info
        if (type === 'tweets') {
            items = items.map(post => {
                const author = userMap[post.author_id] || {};
                return {
                    ...post,
                    _author: {
                        name: author.name || 'Unknown User',
                        username: author.username || 'unknown',
                        profile_image_url: author.profile_image_url || ''
                    }
                };
            });
        }
        
        // For DM conversations, enhance with participant info and messages
        if (type === 'dm_conversations') {
            items = items.map(conv => {
                const participants = (conv.participant_ids || []).map(id => {
                    const user = userMap[id] || {};
                    return {
                        id: id,
                        name: user.name || 'Unknown User',
                        username: user.username || 'unknown',
                        profile_image_url: user.profile_image_url || ''
                    };
                });
                
                // Get messages for this conversation
                const messages = (dmEventsMap[conv.id] || []).map((event, idx) => {
                    const sender = userMap[event.sender_id] || {};
                    return {
                        ...event,
                        _sender: {
                            id: event.sender_id,
                            name: sender.name || 'Unknown User',
                            username: sender.username || 'unknown',
                            profile_image_url: sender.profile_image_url || ''
                        }
                    };
                });
                
                return {
                    ...conv,
                    _participants: participants,
                    _messages: messages
                };
            });
        }
        
        // Render results
        if (items.length === 0) {
            const typeLabel = type === 'tweets' ? 'Posts' : 
                             type === 'dm_conversations' ? 'DMs' : 
                             type === 'search_stream_rules' ? 'Stream Rules' :
                             type.charAt(0).toUpperCase() + type.slice(1);
            content.innerHTML = `<div class="empty-state">No ${typeLabel.toLowerCase()} found${searchQuery ? ` matching "${escapeHtml(searchQuery)}"` : ''}</div>`;
            return;
        }
        
        // Store in cache for filtering
        const typeLabel = type === 'tweets' ? 'Posts' : 
                         type === 'dm_conversations' ? 'DMs' : 
                         type === 'search_stream_rules' ? 'Stream Rules' :
                         type.charAt(0).toUpperCase() + type.slice(1);
        explorerDataCache = {
            items: items,
            type: type,
            title: typeLabel,
            userMap: userMap,
            dmEventsMap: dmEventsMap
        };
        
        // Render results using existing render logic
        explorerPagination.currentPage = 1;
        explorerPagination.totalItems = items.length;
        filterExplorerData();
        
    } catch (error) {
        content.innerHTML = `<div style="color: var(--danger);">Failed to load data: ${error.message}</div>`;
    }
}

async function loadExplorerData() {
    const typeSelect = document.getElementById('explorerType');
    const unifiedTypeSelect = document.getElementById('unifiedExplorerType');
    const type = unifiedTypeSelect?.value || (typeSelect ? typeSelect.value : 'users');
    const searchQuery = document.getElementById('unifiedExplorerSearch')?.value.trim().toLowerCase() || '';
    
    // Use unified search function
    return loadExplorerDataWithSearch(searchQuery, type);
    
    try {
        const response = await fetch(`${API_BASE_URL}/state/export`);
        const data = await response.json();
        
        let items = [];
        let title = '';
        let userMap = {}; // For tweet author lookup
        
        // Build user map for tweet and DM rendering
        if ((type === 'tweets' || type === 'dm_conversations') && data.users) {
            userMap = data.users;
        }
        
        // Build DM events map for conversation rendering
        let dmEventsMap = {};
        if (type === 'dm_conversations' && data.dm_events) {
            // Group DM events by conversation ID
            Object.values(data.dm_events).forEach(event => {
                if (!dmEventsMap[event.dm_conversation_id]) {
                    dmEventsMap[event.dm_conversation_id] = [];
                }
                dmEventsMap[event.dm_conversation_id].push(event);
            });
            // Sort messages within each conversation by created_at
            Object.keys(dmEventsMap).forEach(convID => {
                dmEventsMap[convID].sort((a, b) => {
                    const dateA = new Date(a.created_at || 0);
                    const dateB = new Date(b.created_at || 0);
                    return dateA - dateB; // Oldest first (chronological order)
                });
            });
        }
        
        switch (type) {
            case 'users':
                items = deduplicateById(Object.values(data.users || {}));
                title = 'Users';
                break;
            case 'tweets':
                items = deduplicateById(Object.values(data.tweets || {}));
                title = 'Posts';
                // Sort by created_at (newest first)
                items.sort((a, b) => {
                    const dateA = new Date(a.created_at || 0);
                    const dateB = new Date(b.created_at || 0);
                    return dateB - dateA;
                });
                break;
            case 'lists':
                items = deduplicateById(Object.values(data.lists || {}));
                title = 'Lists';
                break;
            case 'spaces':
                items = deduplicateById(Object.values(data.spaces || {}));
                title = 'Spaces';
                break;
            case 'media':
                items = deduplicateById(Object.values(data.media || {}));
                title = 'Media';
                break;
            case 'news':
                items = deduplicateById(Object.values(data.news || {}));
                title = 'News';
                // Sort by updated_at (newest first)
                items.sort((a, b) => {
                    const dateA = new Date(a.updated_at || 0);
                    const dateB = new Date(b.updated_at || 0);
                    return dateB - dateA;
                });
                break;
            case 'dm_conversations':
                items = deduplicateById(Object.values(data.dm_conversations || {}));
                title = 'DMs';
                // Sort by created_at (newest first)
                items.sort((a, b) => {
                    const dateA = new Date(a.created_at || 0);
                    const dateB = new Date(b.created_at || 0);
                    return dateB - dateA;
                });
                break;
        }
        
        // For posts, enhance with author info
        if (type === 'tweets') {
            items = items.map(post => {
                const author = userMap[post.author_id] || {};
                return {
                    ...post,
                    _author: {
                        name: author.name || 'Unknown User',
                        username: author.username || 'unknown',
                        profile_image_url: author.profile_image_url || ''
                    }
                };
            });
        }
        
        // For DM conversations, enhance with participant info and messages
        if (type === 'dm_conversations') {
            items = items.map(conv => {
                const participants = (conv.participant_ids || []).map(id => {
                    const user = userMap[id] || {};
                    return {
                        id: id,
                        name: user.name || 'Unknown User',
                        username: user.username || 'unknown',
                        profile_image_url: user.profile_image_url || ''
                    };
                });
                
                // Get messages for this conversation
                const messages = (dmEventsMap[conv.id] || []).map((event, idx) => {
                    const sender = userMap[event.sender_id] || {};
                    return {
                        ...event,
                        _sender: {
                            id: event.sender_id,
                            name: sender.name || 'Unknown User',
                            username: sender.username || 'unknown',
                            profile_image_url: sender.profile_image_url || ''
                        }
                    };
                });
                
                return {
                    ...conv,
                    _participants: participants,
                    _messages: messages
                };
            });
        }
        
        // Cache the data for filtering
        explorerDataCache = {
            items: items,
            type: type,
            title: title,
            userMap: userMap,
            dmEventsMap: dmEventsMap
        };
        
        // Reset pagination
        explorerPagination.currentPage = 1;
        explorerPagination.totalItems = items.length;
        
        // Apply search filter if there's a search query
        filterExplorerData();
    } catch (error) {
        content.innerHTML = `<div style="color: var(--danger);">Failed to load data: ${error.message}</div>`;
    }
}

function renderExplorerResults(groupedByType, userMap, content) {
    // No-op: rendering handled by filterExplorerData
}

function filterExplorerData() {
    // Check both old and new search inputs
    const oldSearch = document.getElementById('explorerSearch');
    const newSearch = document.getElementById('unifiedExplorerSearch');
    const searchQuery = (newSearch?.value.trim().toLowerCase() || oldSearch?.value.trim().toLowerCase() || '').toLowerCase();
    const content = document.getElementById('explorerContent');
    
    if (!explorerDataCache.items || explorerDataCache.items.length === 0) {
        return; // Data not loaded yet
    }
    
    let filteredItems = explorerDataCache.items;
    
    // Apply search filter
    if (searchQuery) {
        filteredItems = explorerDataCache.items.filter(item => {
            const itemType = item._type || explorerDataCache.type;
            // For relationships, use matchesSearchWithOperators to support operators like user:, type:, etc.
            if (itemType === 'relationships' || explorerDataCache.type === 'relationships') {
                // Use cached maps for operator matching
                const dataForOperators = {
                    users: explorerDataCache.userMap || {},
                    tweets: explorerDataCache.tweetsMap || {},
                    lists: explorerDataCache.listsMap || {}
                };
                return matchesSearchWithOperators(item, searchQuery, 'relationships', dataForOperators);
            }
            return matchesSearch(item, searchQuery, itemType === 'all' ? 'users' : itemType);
        });
        // Only reset to first page when search query actually changed (not when paginating)
        const currentSearch = searchQuery;
        const lastSearch = explorerDataCache.lastSearchQuery || '';
        if (currentSearch !== lastSearch) {
            explorerPagination.currentPage = 1;
            explorerDataCache.lastSearchQuery = currentSearch;
        }
    } else {
        // Clear last search query when search is cleared
        explorerDataCache.lastSearchQuery = '';
    }
    
    explorerPagination.totalItems = filteredItems.length;
    const totalPages = Math.ceil(filteredItems.length / explorerPagination.itemsPerPage);
    const startIndex = (explorerPagination.currentPage - 1) * explorerPagination.itemsPerPage;
    const endIndex = startIndex + explorerPagination.itemsPerPage;
    const paginatedItems = filteredItems.slice(startIndex, endIndex);
    
    if (filteredItems.length === 0) {
        content.innerHTML = `
            <div class="empty-state">
                ${searchQuery ? `No ${explorerDataCache.title.toLowerCase()} found matching "${escapeHtml(searchQuery)}"` : `No ${explorerDataCache.title.toLowerCase()} found`}
            </div>
        `;
        return;
    }
    
    content.innerHTML = `
        <div style="margin-bottom: 16px; font-size: 14px; color: var(--text-muted); padding: 0 4px;">
            ${searchQuery ? 
                `Showing ${startIndex + 1}-${Math.min(endIndex, filteredItems.length)} of ${filteredItems.length} ${explorerDataCache.title.toLowerCase()} matching "${escapeHtml(searchQuery)}"` :
                `Showing ${startIndex + 1}-${Math.min(endIndex, filteredItems.length)} of ${filteredItems.length} ${explorerDataCache.title.toLowerCase()}`
            }
        </div>
        <div class="${explorerViewMode === 'json' ? 'json-feed' : 'twitter-feed'}">
            ${paginatedItems.map((item, idx) => renderExplorerItem(explorerDataCache.type, item, startIndex + idx)).join('')}
        </div>
        ${totalPages > 1 ? `
            <div class="pagination-container" style="margin-top: 24px; padding-top: 24px; border-top: 1px solid var(--border); display: flex; justify-content: center; align-items: center; gap: 16px;">
                <button class="btn btn-secondary" onclick="goToPage(${explorerPagination.currentPage - 1})" ${explorerPagination.currentPage === 1 ? 'disabled' : ''}>
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <polyline points="15 18 9 12 15 6"></polyline>
                    </svg>
                    Previous
                </button>
                <span class="pagination-info">Page ${explorerPagination.currentPage} of ${totalPages}</span>
                <button class="btn btn-secondary" onclick="goToPage(${explorerPagination.currentPage + 1})" ${explorerPagination.currentPage === totalPages ? 'disabled' : ''}>
                    Next
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <polyline points="9 18 15 12 9 6"></polyline>
                    </svg>
                </button>
            </div>
        ` : ''}
    `;
}

function goToPage(page) {
    if (page < 1) {
        return; // Invalid page
    }
    explorerPagination.currentPage = page;
    filterExplorerData();
    // After filtering, validate and correct page if needed
    const totalPages = Math.ceil(explorerPagination.totalItems / explorerPagination.itemsPerPage);
    if (explorerPagination.currentPage > totalPages && totalPages > 0) {
        explorerPagination.currentPage = totalPages;
        filterExplorerData();
    }
    // Scroll to top of explorer content
    document.getElementById('explorerContent').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

// Enhanced search with operators (e.g., "user:john", "type:bookmark", "id:123")
function matchesSearchWithOperators(item, query, type, data) {
    const lowerQuery = query.toLowerCase().trim();
    
    // Check for search operators
    const operators = {
        user: null,
        username: null,
        tweet: null,
        post: null,
        list: null,
        type: null,
        id: null
    };
    
    // Parse operators (format: "operator:value")
    const operatorPattern = /(\w+):([^\s]+)/g;
    let match;
    let remainingQuery = lowerQuery;
    
    while ((match = operatorPattern.exec(lowerQuery)) !== null) {
        const op = match[1].toLowerCase();
        const value = match[2].toLowerCase();
        if (operators.hasOwnProperty(op)) {
            operators[op] = value;
            remainingQuery = remainingQuery.replace(match[0], '').trim();
        }
    }
    
    // Apply operator filters
    if (operators.user || operators.username) {
        const searchUser = operators.user || operators.username;
        if (type === 'relationships') {
            const userMatch = (item.user_id && item.user_id.toLowerCase().includes(searchUser)) ||
                            (item._user && item._user.username && item._user.username.toLowerCase().includes(searchUser)) ||
                            (item._user && item._user.name && item._user.name.toLowerCase().includes(searchUser));
            if (!userMatch) return false;
        } else if (type === 'tweets') {
            const authorMatch = (item.author_id && item.author_id.toLowerCase().includes(searchUser)) ||
                              (item._author && item._author.username && item._author.username.toLowerCase().includes(searchUser)) ||
                              (item._author && item._author.name && item._author.name.toLowerCase().includes(searchUser));
            if (!authorMatch) return false;
        }
    }
    
    if (operators.tweet || operators.post) {
        const searchPost = operators.tweet || operators.post;
        if (type === 'relationships') {
            if (!item.target_tweet_id || !item.target_tweet_id.toLowerCase().includes(searchPost)) {
                return false;
            }
        } else if (type === 'tweets') {
            if (!item.id || !item.id.toLowerCase().includes(searchPost)) {
                return false;
            }
        }
    }
    
    if (operators.list) {
        const searchList = operators.list;
        if (type === 'relationships') {
            if (!item.target_list_id || !item.target_list_id.toLowerCase().includes(searchList)) {
                return false;
            }
        } else if (type === 'lists') {
            if (!item.id || !item.id.toLowerCase().includes(searchList)) {
                return false;
            }
        }
    }
    
    if (operators.type) {
        const searchType = operators.type;
        if (type === 'relationships') {
            if (!item.type || item.type.toLowerCase() !== searchType) {
                return false;
            }
        }
    }
    
    if (operators.id) {
        const searchID = operators.id;
        if (!item.id || !item.id.toLowerCase().includes(searchID)) {
            return false;
        }
    }
    
    // If there's remaining query text, use regular search
    if (remainingQuery) {
        return matchesSearch(item, remainingQuery, type);
    }
    
    // If only operators were used and they all matched, return true
    const hasOperators = Object.values(operators).some(v => v !== null);
    return hasOperators;
}

function matchesSearch(item, query, type) {
    const lowerQuery = query.toLowerCase();
    
    switch (type) {
        case 'users':
            // Search by ID, username, name, description
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.username && item.username.toLowerCase().includes(lowerQuery)) ||
                (item.name && item.name.toLowerCase().includes(lowerQuery)) ||
                (item.description && item.description.toLowerCase().includes(lowerQuery))
            );
        case 'tweets':
            // Search by ID, text, author name, author username
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.text && item.text.toLowerCase().includes(lowerQuery)) ||
                (item._author && item._author.name && item._author.name.toLowerCase().includes(lowerQuery)) ||
                (item._author && item._author.username && item._author.username.toLowerCase().includes(lowerQuery))
            );
        case 'lists':
            // Search by ID, name, description
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.name && item.name.toLowerCase().includes(lowerQuery)) ||
                (item.description && item.description.toLowerCase().includes(lowerQuery))
            );
        case 'spaces':
            // Search by ID, title
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.title && item.title.toLowerCase().includes(lowerQuery))
            );
        case 'media':
            // Search by ID, type, state
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.type && item.type.toLowerCase().includes(lowerQuery)) ||
                (item.state && item.state.toLowerCase().includes(lowerQuery))
            );
        case 'dm_conversations':
            // Search by conversation ID, participant names/usernames, message text
            const participantMatch = item._participants && item._participants.some(p => 
                (p.name && p.name.toLowerCase().includes(lowerQuery)) ||
                (p.username && p.username.toLowerCase().includes(lowerQuery)) ||
                (p.id && p.id.toLowerCase().includes(lowerQuery))
            );
            const messageMatch = item._messages && item._messages.some(msg =>
                (msg.text && msg.text.toLowerCase().includes(lowerQuery)) ||
                (msg.id && msg.id.toLowerCase().includes(lowerQuery))
            );
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                participantMatch ||
                messageMatch
            );
        case 'search_stream_rules':
            // Search by ID, value, tag
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.value && item.value.toLowerCase().includes(lowerQuery)) ||
                (item.tag && item.tag.toLowerCase().includes(lowerQuery))
            );
        case 'relationships':
            // Search by user info, target user info, post ID, list ID
            return (
                (item.user_id && item.user_id.toLowerCase().includes(lowerQuery)) ||
                (item.target_user_id && item.target_user_id.toLowerCase().includes(lowerQuery)) ||
                (item.target_tweet_id && item.target_tweet_id.toLowerCase().includes(lowerQuery)) ||
                (item.target_list_id && item.target_list_id.toLowerCase().includes(lowerQuery)) ||
                (item.type && item.type.toLowerCase().includes(lowerQuery)) ||
                (item._user && item._user.username && item._user.username.toLowerCase().includes(lowerQuery)) ||
                (item._user && item._user.name && item._user.name.toLowerCase().includes(lowerQuery)) ||
                (item._target_user && item._target_user.username && item._target_user.username.toLowerCase().includes(lowerQuery)) ||
                (item._target_user && item._target_user.name && item._target_user.name.toLowerCase().includes(lowerQuery)) ||
                (item._target_tweet && item._target_tweet.text && item._target_tweet.text.toLowerCase().includes(lowerQuery)) ||
                (item._target_list && item._target_list.name && item._target_list.name.toLowerCase().includes(lowerQuery))
            );
        case 'communities':
            // Search by ID, name, description
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.name && item.name.toLowerCase().includes(lowerQuery)) ||
                (item.description && item.description.toLowerCase().includes(lowerQuery))
            );
        case 'communities':
            // Search by ID, name, description
            return (
                (item.id && item.id.toLowerCase().includes(lowerQuery)) ||
                (item.name && item.name.toLowerCase().includes(lowerQuery)) ||
                (item.description && item.description.toLowerCase().includes(lowerQuery))
            );
        default:
            // Fallback: search in all string fields
            return JSON.stringify(item).toLowerCase().includes(lowerQuery);
    }
}

// Map dropdown filter values to actual relationship types
function mapFilterToRelationshipType(filterValue) {
    const mapping = {
        'all': 'all',
        'bookmarks': 'bookmark',
        'likes': 'like',
        'following': 'following',
        'followers': 'follower',
        'reposts': 'retweet',
        'muting': 'mute',
        'blocking': 'block',
        'list_memberships': 'list_member',
        'followed_lists': 'followed_list',
        'pinned_lists': 'pinned_list'
    };
    return mapping[filterValue] || filterValue;
}

// Extract relationships from state export
function extractRelationships(data, relationshipTypeFilter) {
    const relationships = [];
    
    // Map the filter value to the actual relationship type
    const actualType = mapFilterToRelationshipType(relationshipTypeFilter);
    
    // First, try to use the relationships array if it exists (from new export format)
    if (data.relationships && Array.isArray(data.relationships)) {
        data.relationships.forEach(rel => {
            if (actualType === 'all' || actualType === rel.type) {
                relationships.push(rel);
            }
        });
        return relationships;
    }
    
    // Fallback: extract from users (old format or when relationships not exported)
    const users = data.users || {};
    const tweets = data.tweets || {};
    const lists = data.lists || {};
    
    Object.values(users).forEach(user => {
        // Bookmarks
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'bookmarks') {
            (user.BookmarkedTweets || []).forEach(tweetId => {
                relationships.push({
                    id: `bookmark-${user.id}-${tweetId}`,
                    type: 'bookmark',
                    user_id: user.id,
                    target_tweet_id: tweetId
                });
            });
        }
        
        // Likes
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'likes') {
            (user.LikedTweets || []).forEach(tweetId => {
                relationships.push({
                    id: `like-${user.id}-${tweetId}`,
                    type: 'like',
                    user_id: user.id,
                    target_tweet_id: tweetId
                });
            });
        }
        
        // Following
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'following') {
            (user.Following || []).forEach(targetUserId => {
                relationships.push({
                    id: `following-${user.id}-${targetUserId}`,
                    type: 'following',
                    user_id: user.id,
                    target_user_id: targetUserId
                });
            });
        }
        
        // Followers (reverse relationship)
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'followers') {
            (user.Followers || []).forEach(followerId => {
                relationships.push({
                    id: `follower-${followerId}-${user.id}`,
                    type: 'follower',
                    user_id: followerId,
                    target_user_id: user.id
                });
            });
        }
        
        // Reposts
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'reposts') {
            (user.RetweetedTweets || []).forEach(tweetId => {
                relationships.push({
                    id: `retweet-${user.id}-${tweetId}`,
                    type: 'retweet',
                    user_id: user.id,
                    target_tweet_id: tweetId
                });
            });
        }
        
        // Muting
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'muting') {
            (user.MutedUsers || []).forEach(targetUserId => {
                relationships.push({
                    id: `mute-${user.id}-${targetUserId}`,
                    type: 'mute',
                    user_id: user.id,
                    target_user_id: targetUserId
                });
            });
        }
        
        // Blocking
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'blocking') {
            (user.BlockedUsers || []).forEach(targetUserId => {
                relationships.push({
                    id: `block-${user.id}-${targetUserId}`,
                    type: 'block',
                    user_id: user.id,
                    target_user_id: targetUserId
                });
            });
        }
        
        // List Memberships
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'list_memberships') {
            (user.ListMemberships || []).forEach(listId => {
                relationships.push({
                    id: `list_member-${user.id}-${listId}`,
                    type: 'list_member',
                    user_id: user.id,
                    target_list_id: listId
                });
            });
        }
        
        // Followed Lists
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'followed_lists') {
            (user.FollowedLists || []).forEach(listId => {
                relationships.push({
                    id: `followed_list-${user.id}-${listId}`,
                    type: 'followed_list',
                    user_id: user.id,
                    target_list_id: listId
                });
            });
        }
        
        // Pinned Lists
        if (relationshipTypeFilter === 'all' || relationshipTypeFilter === 'pinned_lists') {
            (user.PinnedLists || []).forEach(listId => {
                relationships.push({
                    id: `pinned_list-${user.id}-${listId}`,
                    type: 'pinned_list',
                    user_id: user.id,
                    target_list_id: listId
                });
            });
        }
    });
    
    return relationships;
}

function renderExplorerItem(type, item, index) {
    // Create a unique ID for this item (use item.id if available, otherwise use index)
    const itemId = (item.id || `item-${index}`).replace(/[^a-zA-Z0-9]/g, '-');
    
    // If JSON view mode, render raw JSON
    if (explorerViewMode === 'json') {
        // Remove internal helper fields (_author, _participants, _messages, _sender, _user, _target_*) for cleaner JSON
        const cleanItem = { ...item };
        delete cleanItem._author;
        delete cleanItem._sender;
        delete cleanItem._user;
        delete cleanItem._target_user;
        delete cleanItem._target_tweet;
        delete cleanItem._target_list;
        
        // For DM conversations, include the messages in the JSON
        if (type === 'dm_conversations' && item._messages) {
            // Include messages but clean them up (remove _sender)
            cleanItem.messages = item._messages.map(msg => {
                const cleanMsg = { ...msg };
                delete cleanMsg._sender;
                return cleanMsg;
            });
        }
        
        // Remove internal helper fields
        delete cleanItem._participants;
        delete cleanItem._messages;
        
        return `
            <div class="json-item-container">
                <div class="json-item-header" onclick="toggleJsonItem('${itemId}')">
                    <span class="json-item-type">${type}</span>
                    <span class="json-item-id">ID: ${item.id || 'N/A'}</span>
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="json-toggle-icon" id="json-toggle-${itemId}">
                        <polyline points="6 9 12 15 18 9"></polyline>
                    </svg>
                </div>
                <div class="json-item-content" id="json-content-${itemId}" style="display: none;">
                    <pre class="json-code-block">${syntaxHighlight(JSON.stringify(cleanItem, null, 2))}</pre>
                </div>
            </div>
        `;
    }
    
    // Formatted view mode
    let formattedCard = '';
    switch (type) {
        case 'users':
            formattedCard = renderUserCard(item);
            break;
        case 'tweets':
            formattedCard = renderTweetCard(item);
            break;
        case 'lists':
            formattedCard = renderListCard(item);
            break;
        case 'spaces':
            formattedCard = renderSpaceCard(item);
            break;
        case 'media':
            formattedCard = renderMediaCard(item);
            break;
        case 'dm_conversations':
            formattedCard = renderDMConversationCard(item);
            break;
            case 'communities':
                formattedCard = renderCommunityCard(item);
                break;
            case 'news':
                formattedCard = renderNewsCard(item);
                break;
            case 'search_stream_rules':
                formattedCard = renderStreamRuleCard(item);
                break;
            case 'relationships':
                formattedCard = renderRelationshipCard(item);
                break;
            default:
                formattedCard = `<div class="data-item">${JSON.stringify(item, null, 2)}</div>`;
        }
    
    // Wrap formatted card with expandable JSON section
    const cleanItem = { ...item };
    delete cleanItem._author;
    delete cleanItem._sender;
    
    // For DM conversations, include the messages in the JSON
    if (type === 'dm_conversations' && item._messages) {
        // Include messages but clean them up (remove _sender)
        cleanItem.messages = item._messages.map(msg => {
            const cleanMsg = { ...msg };
            delete cleanMsg._sender;
            return cleanMsg;
        });
    }
    
    // Remove internal helper fields
    delete cleanItem._participants;
    delete cleanItem._messages;
    
    return `
        <div class="explorer-item-wrapper">
            ${formattedCard}
            <div class="json-expand-section">
                <button class="json-expand-btn" onclick="toggleItemJson('${itemId}')">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="json-expand-icon" id="json-expand-icon-${itemId}">
                        <polyline points="6 9 12 15 18 9"></polyline>
                    </svg>
                    <span>View Raw JSON</span>
                </button>
                <div class="json-expand-content" id="json-expand-content-${itemId}" style="display: none;">
                    <pre class="json-code-block">${syntaxHighlight(JSON.stringify(cleanItem, null, 2))}</pre>
                </div>
            </div>
        </div>
    `;
}

function toggleExplorerView() {
    explorerViewMode = explorerViewMode === 'formatted' ? 'json' : 'formatted';
    const icon = document.getElementById('explorerViewIcon');
    const label = document.getElementById('explorerViewLabel');
    
    if (explorerViewMode === 'json') {
        label.textContent = 'Formatted View';
        icon.innerHTML = '<polyline points="16 18 22 12 16 6"></polyline><polyline points="8 6 2 12 8 18"></polyline>';
    } else {
        label.textContent = 'JSON View';
        icon.innerHTML = '<polyline points="16 18 22 12 16 6"></polyline><polyline points="8 6 2 12 8 18"></polyline>';
    }
    
    // Re-render the current view
    filterExplorerData();
}

function toggleJsonItem(itemId) {
    const contentDiv = document.getElementById(`json-content-${itemId}`);
    const toggleIcon = document.getElementById(`json-toggle-${itemId}`);
    
    if (contentDiv && toggleIcon) {
        if (contentDiv.style.display === 'none') {
            contentDiv.style.display = 'block';
            toggleIcon.style.transform = 'rotate(180deg)';
        } else {
            contentDiv.style.display = 'none';
            toggleIcon.style.transform = 'rotate(0deg)';
        }
    }
}

function toggleItemJson(itemId) {
    const contentDiv = document.getElementById(`json-expand-content-${itemId}`);
    const toggleIcon = document.getElementById(`json-expand-icon-${itemId}`);
    
    if (contentDiv && toggleIcon) {
        if (contentDiv.style.display === 'none') {
            contentDiv.style.display = 'block';
            toggleIcon.style.transform = 'rotate(180deg)';
        } else {
            contentDiv.style.display = 'none';
            toggleIcon.style.transform = 'rotate(0deg)';
        }
    }
}

function renderUserCard(user) {
    const verifiedBadge = user.verified ? `
        <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" style="margin-left: 4px; color: var(--primary);">
            <circle cx="12" cy="12" r="10" fill="currentColor" opacity="0.1"/>
            <polyline points="9 12 11 14 15 10" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
    ` : '';
    
    const protectedIcon = user.protected ? `
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="margin-left: 8px; opacity: 0.7;">
            <rect x="3" y="11" width="18" height="11" rx="2" ry="2" stroke="currentColor" fill="none"/>
            <path d="M7 11V7a5 5 0 0 1 10 0v4" stroke="currentColor" fill="none"/>
        </svg>
    ` : '';
    
    const metrics = user.public_metrics || {};
    const location = user.location ? `<div style="color: var(--text-muted); font-size: 14px; margin-top: 4px;">📍 ${escapeHtml(user.location)}</div>` : '';
    const url = user.url ? `<div style="color: var(--primary); font-size: 14px; margin-top: 4px;"><a href="${escapeHtml(user.url)}" target="_blank" style="color: var(--primary); text-decoration: none;">${escapeHtml(user.url)}</a></div>` : '';
    
    return `
        <div class="twitter-card user-card">
            <div class="user-header">
                <div class="user-avatar">
                    ${user.profile_image_url ? 
                        `<img src="${escapeHtml(user.profile_image_url)}" alt="${escapeHtml(user.name || 'User')}" onerror="this.src='data:image/svg+xml,%3Csvg xmlns=%27http://www.w3.org/2000/svg%27 viewBox=%270 0 24 24%27%3E%3Ccircle cx=%2712%27 cy=%2712%27 r=%2710%27 fill=%27%23ccc%27/%3E%3C/svg%3E'">` :
                        `<div class="avatar-placeholder">${(user.name || 'U')[0].toUpperCase()}</div>`
                    }
                </div>
                <div class="user-info">
                    <div class="user-name-row">
                        <span class="user-name">${escapeHtml(user.name || 'Unknown User')}</span>
                        ${verifiedBadge}
                        ${protectedIcon}
                        <span class="item-id-display">ID: ${escapeHtml(user.id || 'N/A')}</span>
                    </div>
                    <div class="user-handle">@${escapeHtml(user.username || 'unknown')}</div>
                    ${user.description ? `<div class="user-bio">${escapeHtml(user.description)}</div>` : ''}
                    ${location}
                    ${url}
                    <div class="user-metrics">
                        <span><strong>${formatNumber(metrics.following_count || 0)}</strong> Following</span>
                        <span><strong>${formatNumber(metrics.followers_count || 0)}</strong> Followers</span>
                        <span><strong>${formatNumber(metrics.tweet_count || 0)}</strong> Posts</span>
                    </div>
                </div>
            </div>
        </div>
    `;
}

function renderTweetCard(post) {
    const author = post._author || { name: 'Unknown User', username: 'unknown', profile_image_url: '' };
    const authorName = author.name || 'Unknown User';
    const authorHandle = author.username || 'unknown';
    const authorAvatar = author.profile_image_url || '';
    
    const metrics = post.public_metrics || {};
    const createdAt = formatTweetTime(post.created_at);
    
    return `
        <div class="twitter-card tweet-card">
            <div class="tweet-header">
                <div class="tweet-avatar">
                    ${authorAvatar ? 
                        `<img src="${escapeHtml(authorAvatar)}" alt="${escapeHtml(authorName)}" onerror="this.src='data:image/svg+xml,%3Csvg xmlns=%27http://www.w3.org/2000/svg%27 viewBox=%270 0 24 24%27%3E%3Ccircle cx=%2712%27 cy=%2712%27 r=%2710%27 fill=%27%23444%27/%3E%3C/svg%3E'">` :
                        `<div class="avatar-placeholder small">${authorName[0]}</div>`
                    }
                </div>
                <div class="tweet-content">
                    <div class="tweet-author">
                        <span class="tweet-author-name">${escapeHtml(authorName)}</span>
                        <span class="tweet-author-handle">@${escapeHtml(authorHandle)}</span>
                        <span class="tweet-time">· ${createdAt}</span>
                        <span class="item-id-display">ID: ${escapeHtml(post.id || 'N/A')}</span>
                    </div>
                    <div class="tweet-text">${escapeHtml(post.text || '')}</div>
                    <div class="tweet-actions">
                        <button class="tweet-action" title="Reply">
                            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
                            </svg>
                            <span>${formatNumber(metrics.reply_count || 0)}</span>
                        </button>
                        <button class="tweet-action" title="Repost">
                            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <path d="M17 1l4 4-4 4"></path>
                                <path d="M3 11V9a4 4 0 0 1 4-4h14"></path>
                                <path d="M7 23l-4-4 4-4"></path>
                                <path d="M21 13v2a4 4 0 0 1-4 4H3"></path>
                            </svg>
                            <span>${formatNumber(metrics.retweet_count || 0)}</span>
                        </button>
                        <button class="tweet-action" title="Like">
                            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
                            </svg>
                            <span>${formatNumber(metrics.like_count || 0)}</span>
                        </button>
                        <button class="tweet-action" title="Share">
                            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                <path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8"></path>
                                <polyline points="16 6 12 2 8 6"></polyline>
                                <line x1="12" y1="2" x2="12" y2="15"></line>
                            </svg>
                        </button>
                    </div>
                </div>
            </div>
        </div>
    `;
}

function renderListCard(list) {
    const memberCount = list.member_count || 0;
    const followerCount = list.follower_count || 0;
    const isPrivate = list.private ? '🔒 Private' : '🌐 Public';
    
    return `
        <div class="twitter-card list-card">
            <div class="list-header">
                <div class="list-icon">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="8" y1="6" x2="21" y2="6"></line>
                        <line x1="8" y1="12" x2="21" y2="12"></line>
                        <line x1="8" y1="18" x2="21" y2="18"></line>
                        <line x1="3" y1="6" x2="3.01" y2="6"></line>
                        <line x1="3" y1="12" x2="3.01" y2="12"></line>
                        <line x1="3" y1="18" x2="3.01" y2="18"></line>
                    </svg>
                </div>
                <div class="list-info">
                    <div class="list-name-row">
                        <span class="list-name">${escapeHtml(list.name || 'Unnamed List')}</span>
                        <span class="list-privacy">${isPrivate}</span>
                        <span class="item-id-display">ID: ${escapeHtml(list.id || 'N/A')}</span>
                    </div>
                    ${list.description ? `<div class="list-description">${escapeHtml(list.description)}</div>` : ''}
                    <div class="list-metrics">
                        <span><strong>${formatNumber(memberCount)}</strong> Members</span>
                        <span><strong>${formatNumber(followerCount)}</strong> Followers</span>
                    </div>
                </div>
            </div>
        </div>
    `;
}

function renderRelationshipCard(rel) {
    const user = rel._user || {};
    const targetUser = rel._target_user || null;
    const targetPost = rel._target_tweet || null;
    const targetList = rel._target_list || null;
    
    // Relationship type labels and action verbs
    const typeInfo = {
        'bookmark': { action: 'bookmarked', icon: '🔖', color: 'var(--primary)' },
        'like': { action: 'liked', icon: '❤️', color: 'var(--danger)' },
        'following': { action: 'followed', icon: '➕', color: 'var(--primary)' },
        'follower': { action: 'follows', icon: '👤', color: 'var(--success)' },
        'retweet': { action: 'reposted', icon: '🔄', color: 'var(--success)' },
        'mute': { action: 'muted', icon: '🔇', color: 'var(--warning)' },
        'block': { action: 'blocked', icon: '🚫', color: 'var(--danger)' },
        'list_member': { action: 'is a member of', icon: '📋', color: 'var(--primary)' },
        'followed_list': { action: 'follows', icon: '📌', color: 'var(--primary)' },
        'pinned_list': { action: 'pinned', icon: '📌', color: 'var(--warning)' }
    };
    
    const info = typeInfo[rel.type] || { action: rel.type, icon: '🔗', color: 'var(--text-muted)' };
    
    // Build relationship text and target entity card
    let relationshipText = '';
    let targetEntityCard = '';
    
    if (targetUser) {
        relationshipText = `<strong style="color: var(--text-primary);">${escapeHtml(user.name || 'Unknown User')}</strong> <span style="color: ${info.color};">${info.action}</span> <strong style="color: var(--text-primary);">${escapeHtml(targetUser.name || 'Unknown User')}</strong>`;
        targetEntityCard = `
            <div style="flex: 1; min-width: 0;">
                <div style="display: flex; align-items: center; gap: 8px; padding: 8px; background: var(--bg-tertiary); border-radius: 8px;">
                    ${targetUser.profile_image_url ? 
                        `<img src="${escapeHtml(targetUser.profile_image_url)}" alt="${escapeHtml(targetUser.name)}" style="width: 32px; height: 32px; border-radius: 50%;" onerror="this.style.display='none'">` :
                        `<div style="width: 32px; height: 32px; border-radius: 50%; background: var(--bg-secondary); display: flex; align-items: center; justify-content: center; font-weight: 600; font-size: 14px;">${(targetUser.name || 'U')[0].toUpperCase()}</div>`
                    }
                    <div style="flex: 1; min-width: 0;">
                        <div style="font-weight: 600;">${escapeHtml(targetUser.name || 'Unknown User')}</div>
                        <div style="color: var(--text-muted); font-size: 13px;">@${escapeHtml(targetUser.username || 'unknown')}</div>
                    </div>
                    <div style="color: var(--text-muted); font-size: 12px;">ID: ${escapeHtml(targetUser.id || 'N/A')}</div>
                </div>
            </div>
        `;
    } else if (targetPost) {
        const postText = targetPost.text || '';
        const truncatedText = postText.length > 80 ? postText.substring(0, 80) + '...' : postText;
        relationshipText = `<strong style="color: var(--text-primary);">${escapeHtml(user.name || 'Unknown User')}</strong> <span style="color: ${info.color};">${info.action}</span> <strong style="color: var(--text-primary);">Post ${escapeHtml(targetPost.id || 'N/A')}</strong>`;
        targetEntityCard = `
            <div style="flex: 1; min-width: 0;">
                <div style="display: flex; align-items: center; gap: 8px; padding: 8px; background: var(--bg-tertiary); border-radius: 8px;">
                    <div style="width: 32px; height: 32px; border-radius: 50%; background: var(--bg-secondary); display: flex; align-items: center; justify-content: center; font-weight: 600; font-size: 14px; flex-shrink: 0;">📝</div>
                    <div style="flex: 1; min-width: 0;">
                        <div style="font-weight: 600;">Post ${escapeHtml(targetPost.id || 'N/A')}</div>
                        <div style="color: var(--text-muted); font-size: 13px;">${escapeHtml(truncatedText)}</div>
                    </div>
                    <div style="color: var(--text-muted); font-size: 12px;">ID: ${escapeHtml(targetPost.id || 'N/A')}</div>
                </div>
            </div>
        `;
    } else if (targetList) {
        relationshipText = `<strong style="color: var(--text-primary);">${escapeHtml(user.name || 'Unknown User')}</strong> <span style="color: ${info.color};">${info.action}</span> <strong style="color: var(--text-primary);">${escapeHtml(targetList.name || 'Unnamed List')}</strong>`;
        targetEntityCard = `
            <div style="flex: 1; min-width: 0;">
                <div style="display: flex; align-items: center; gap: 8px; padding: 8px; background: var(--bg-tertiary); border-radius: 8px;">
                    <div style="width: 32px; height: 32px; border-radius: 50%; background: var(--bg-secondary); display: flex; align-items: center; justify-content: center; font-weight: 600; font-size: 14px; flex-shrink: 0;">📋</div>
                    <div style="flex: 1; min-width: 0;">
                        <div style="font-weight: 600;">${escapeHtml(targetList.name || 'Unnamed List')}</div>
                        ${targetList.description ? `<div style="color: var(--text-muted); font-size: 13px;">${escapeHtml(targetList.description)}</div>` : '<div style="color: var(--text-muted); font-size: 13px;">&nbsp;</div>'}
                    </div>
                    <div style="color: var(--text-muted); font-size: 12px;">ID: ${escapeHtml(targetList.id || 'N/A')}</div>
                </div>
            </div>
        `;
    }
    
    return `
        <div class="twitter-card relationship-card">
            <div style="margin-bottom: 12px;">
                <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 8px;">
                    <span style="font-size: 20px;">${info.icon}</span>
                    <div style="flex: 1; font-weight: 600;">
                        ${relationshipText}
                    </div>
                    <span style="color: var(--text-muted); font-size: 11px;">ID: ${escapeHtml(rel.id || 'N/A')}</span>
                </div>
            </div>
            <div style="display: flex; gap: 12px; align-items: flex-start;">
                <div style="flex: 1; min-width: 0;">
                    <div style="display: flex; align-items: center; gap: 8px; padding: 8px; background: var(--bg-tertiary); border-radius: 8px;">
                        ${user.profile_image_url ? 
                            `<img src="${escapeHtml(user.profile_image_url)}" alt="${escapeHtml(user.name)}" style="width: 32px; height: 32px; border-radius: 50%;" onerror="this.style.display='none'">` :
                            `<div style="width: 32px; height: 32px; border-radius: 50%; background: var(--bg-secondary); display: flex; align-items: center; justify-content: center; font-weight: 600; font-size: 14px;">${(user.name || 'U')[0].toUpperCase()}</div>`
                        }
                        <div style="flex: 1; min-width: 0;">
                            <div style="font-weight: 600;">${escapeHtml(user.name || 'Unknown User')}</div>
                            <div style="color: var(--text-muted); font-size: 13px;">@${escapeHtml(user.username || 'unknown')}</div>
                        </div>
                        <div style="color: var(--text-muted); font-size: 12px;">ID: ${escapeHtml(user.id || 'N/A')}</div>
                    </div>
                </div>
                ${targetEntityCard}
            </div>
        </div>
    `;
}

function renderCommunityCard(community) {
    const memberCount = community.member_count || 0;
    const createdAt = community.created_at ? formatDate(new Date(community.created_at)) : 'Unknown';
    
    return `
        <div class="twitter-card list-card">
            <div class="list-header">
                <div class="list-icon">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"></path>
                        <circle cx="9" cy="7" r="4"></circle>
                        <path d="M23 21v-2a4 4 0 0 0-3-3.87"></path>
                        <path d="M16 3.13a4 4 0 0 1 0 7.75"></path>
                    </svg>
                </div>
                <div class="list-info">
                    <div class="list-name-row">
                        <span class="list-name">${escapeHtml(community.name || 'Unnamed Community')}</span>
                        <span class="item-id-display">ID: ${escapeHtml(community.id || 'N/A')}</span>
                    </div>
                    ${community.description ? `<div class="list-description">${escapeHtml(community.description)}</div>` : ''}
                    <div class="list-metrics">
                        <span><strong>${formatNumber(memberCount)}</strong> Members</span>
                        <span style="color: var(--text-muted); font-size: 13px;">Created ${createdAt}</span>
                    </div>
                </div>
            </div>
        </div>
    `;
}

function renderSpaceCard(space) {
    const stateColors = {
        'live': '#f4212e',
        'scheduled': '#1d9bf0',
        'ended': '#536471'
    };
    const stateColor = stateColors[space.state] || '#536471';
    
    return `
        <div class="twitter-card space-card">
            <div class="space-header">
                <div class="space-icon" style="background: ${stateColor}20; color: ${stateColor};">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <circle cx="12" cy="12" r="10"></circle>
                        <line x1="12" y1="2" x2="12" y2="6"></line>
                        <line x1="12" y1="18" x2="12" y2="22"></line>
                        <line x1="4.93" y1="4.93" x2="7.76" y2="7.76"></line>
                        <line x1="16.24" y1="16.24" x2="19.07" y2="19.07"></line>
                        <line x1="2" y1="12" x2="6" y2="12"></line>
                        <line x1="18" y1="12" x2="22" y2="12"></line>
                        <line x1="4.93" y1="19.07" x2="7.76" y2="16.24"></line>
                        <line x1="16.24" y1="7.76" x2="19.07" y2="4.93"></line>
                    </svg>
                </div>
                <div class="space-info">
                    <div class="space-title-row">
                        <span class="space-title">${escapeHtml(space.title || 'Untitled Space')}</span>
                        <span class="space-state" style="color: ${stateColor};">
                            ${space.state === 'live' ? '🔴 LIVE' : space.state === 'scheduled' ? '📅 Scheduled' : '✅ Ended'}
                        </span>
                        <span class="item-id-display">ID: ${escapeHtml(space.id || 'N/A')}</span>
                    </div>
                    ${space.created_at ? `<div class="space-time">${formatDate(space.created_at)}</div>` : ''}
                    ${space.participant_count ? `<div class="space-metrics">👥 ${formatNumber(space.participant_count)} participants</div>` : ''}
                </div>
            </div>
        </div>
    `;
}

function renderMediaCard(media) {
    const stateColors = {
        'succeeded': '#00ba7c',
        'processing': '#1d9bf0',
        'failed': '#f4212e'
    };
    const stateColor = stateColors[media.state] || '#536471';
    
    return `
        <div class="twitter-card media-card">
            <div class="media-header">
                <div class="media-icon">
                    ${media.type === 'video' ? '🎥' : media.type === 'photo' ? '📷' : '🖼️'}
                </div>
                <div class="media-info">
                    <div class="media-type-row">
                        <div class="media-type">${escapeHtml(media.type || 'unknown')}</div>
                        <span class="item-id-display">ID: ${escapeHtml(media.id || 'N/A')}</span>
                    </div>
                    <div class="media-state" style="color: ${stateColor};">
                        ${media.state || 'unknown'}
                    </div>
                    ${media.url ? `<div class="media-url"><a href="${escapeHtml(media.url)}" target="_blank" style="color: var(--primary);">View Media</a></div>` : ''}
                </div>
            </div>
        </div>
    `;
}

function renderNewsCard(news) {
    const updatedAt = news.updated_at ? new Date(news.updated_at).toLocaleString() : 'N/A';
    const category = news.category ? `<span class="news-category">${escapeHtml(news.category)}</span>` : '';
    const topics = news.contexts && news.contexts.topics && news.contexts.topics.length > 0 
        ? `<div class="news-topics" style="margin-top: 8px;">
            ${news.contexts.topics.map(topic => `<span class="topic-tag">${escapeHtml(topic)}</span>`).join('')}
           </div>`
        : '';
    const disclaimer = news.disclaimer ? `<div class="news-disclaimer" style="margin-top: 8px; font-size: 12px; color: var(--text-muted); font-style: italic;">${escapeHtml(news.disclaimer)}</div>` : '';
    
    return `
        <div class="twitter-card news-card">
            <div class="news-header">
                <div class="news-title-row">
                    <h3 class="news-title">${escapeHtml(news.name || 'Untitled News')}</h3>
                    <span class="item-id-display">ID: ${escapeHtml(news.id || 'N/A')}</span>
                </div>
                ${category}
            </div>
            ${news.hook ? `<div class="news-hook" style="font-weight: 500; margin-top: 8px; color: var(--text);">${escapeHtml(news.hook)}</div>` : ''}
            ${news.summary ? `<div class="news-summary" style="margin-top: 12px; line-height: 1.6; color: var(--text);">${escapeHtml(news.summary)}</div>` : ''}
            ${topics}
            ${disclaimer}
            <div class="news-footer" style="margin-top: 12px; padding-top: 12px; border-top: 1px solid var(--border); font-size: 12px; color: var(--text-muted);">
                Updated: ${updatedAt}
            </div>
        </div>
    `;
}

function renderStreamRuleCard(rule) {
    return `
        <div class="twitter-card stream-rule-card">
            <div class="stream-rule-header">
                <div class="stream-rule-icon">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
                        <line x1="8" y1="9" x2="16" y2="9"></line>
                        <line x1="8" y1="13" x2="16" y2="13"></line>
                    </svg>
                </div>
                <div class="stream-rule-info" style="flex: 1;">
                    <div class="stream-rule-title-row">
                        <span class="stream-rule-value" style="font-weight: 500; color: var(--text-primary);">
                            ${escapeHtml(rule.value || 'No rule value')}
                        </span>
                        <span class="item-id-display">ID: ${escapeHtml(rule.id || 'N/A')}</span>
                    </div>
                    ${rule.tag ? `<div class="stream-rule-tag" style="margin-top: 8px; color: var(--primary); font-size: 14px;">
                        <span style="font-weight: 500;">Tag:</span> ${escapeHtml(rule.tag)}
                    </div>` : ''}
                </div>
            </div>
        </div>
    `;
}

function renderDMConversationCard(conversation) {
    const participants = conversation._participants || [];
    const participantNames = participants.map(p => p.name || p.username || 'Unknown').join(', ');
    const participantCount = participants.length;
    const createdAt = formatDate(conversation.created_at);
    const messages = conversation._messages || [];
    const messageCount = messages.length;
    const conversationId = conversation.id;
    const isExpanded = false; // Track expansion state
    
    // Get last message preview
    const lastMessage = messages.length > 0 ? messages[messages.length - 1] : null;
    const lastMessagePreview = lastMessage ? 
        (lastMessage.text || lastMessage.message_text || '').substring(0, 100) + (lastMessage.text && lastMessage.text.length > 100 ? '...' : '') : 
        'No messages yet';
    
    return `
        <div class="twitter-card dm-conversation-card" data-conversation-id="${conversationId}">
            <div class="dm-conversation-header" onclick="toggleDMConversation('${conversationId}')" style="cursor: pointer;">
                <div class="dm-icon">
                    <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
                    </svg>
                </div>
                <div class="dm-conversation-info" style="flex: 1;">
                    <div class="dm-conversation-title">
                        ${participantCount > 0 ? escapeHtml(participantNames) : 'Empty Conversation'}
                        <span class="item-id-display">ID: ${escapeHtml(conversation.id || 'N/A')}</span>
                    </div>
                    <div class="dm-conversation-meta">
                        ${messageCount} message${messageCount !== 1 ? 's' : ''}
                        ${createdAt ? ` · ${createdAt}` : ''}
                    </div>
                    ${lastMessage ? `<div class="dm-conversation-preview" style="font-size: 14px; color: var(--text-muted); margin-top: 4px;">${escapeHtml(lastMessagePreview)}</div>` : ''}
                </div>
                <div class="dm-conversation-toggle" style="color: var(--text-muted);">
                    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" id="toggle-icon-${conversationId}">
                        <polyline points="6 9 12 15 18 9"></polyline>
                    </svg>
                </div>
            </div>
            <div class="dm-conversation-messages" id="messages-${conversationId}" style="display: none; margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--border);">
                ${messages.length > 0 ? 
                    messages.map((msg, idx) => {
                        const prevMsg = idx > 0 ? messages[idx - 1] : null;
                        return renderDMEventCard(msg, participants, idx, prevMsg);
                    }).join('') : 
                    '<div style="text-align: center; color: var(--text-muted); padding: 20px;">No messages in this conversation</div>'
                }
            </div>
        </div>
    `;
}

function toggleDMConversation(conversationId) {
    const messagesDiv = document.getElementById(`messages-${conversationId}`);
    const toggleIcon = document.getElementById(`toggle-icon-${conversationId}`);
    
    if (messagesDiv.style.display === 'none') {
        messagesDiv.style.display = 'block';
        toggleIcon.style.transform = 'rotate(180deg)';
    } else {
        messagesDiv.style.display = 'none';
        toggleIcon.style.transform = 'rotate(0deg)';
    }
}

function renderDMEventCard(event, participants, index, prevMsg) {
    const sender = event._sender || { name: 'Unknown User', username: 'unknown', profile_image_url: '', id: event.sender_id };
    const senderName = sender.name || 'Unknown User';
    const senderHandle = sender.username || 'unknown';
    const senderAvatar = sender.profile_image_url || '';
    const senderId = sender.id || event.sender_id || '';
    const createdAt = formatTweetTime(event.created_at);
    
    const text = event.text || event.message_text || '';
    const eventType = event.event_type || 'message_create';
    
    // Determine if this is from the first participant (left side) or second (right side)
    const isFirstParticipant = participants.length > 0 && senderId === participants[0].id;
    const isRightAligned = !isFirstParticipant && participants.length > 1;
    
    // Check if previous message was from same sender (to group consecutive messages)
    const isGrouped = prevMsg && prevMsg.sender_id === senderId;
    
    return `
        <div class="dm-message-wrapper ${isRightAligned ? 'dm-message-right' : 'dm-message-left'} ${isGrouped ? 'dm-message-grouped' : ''}">
            ${!isGrouped ? `
                <div class="dm-message-avatar">
                    ${senderAvatar ? 
                        `<img src="${escapeHtml(senderAvatar)}" alt="${escapeHtml(senderName)}" onerror="this.src='data:image/svg+xml,%3Csvg xmlns=%27http://www.w3.org/2000/svg%27 viewBox=%270 0 24 24%27%3E%3Ccircle cx=%2712%27 cy=%2712%27 r=%2710%27 fill=%27%23444%27/%3E%3C/svg%3E'">` :
                        `<div class="avatar-placeholder small">${senderName[0]}</div>`
                    }
                </div>
            ` : '<div class="dm-message-avatar-spacer"></div>'}
            <div class="dm-message-bubble">
                ${!isGrouped ? `
                    <div class="dm-message-sender">${escapeHtml(senderName)}</div>
                ` : ''}
                <div class="dm-message-text">${escapeHtml(text)}</div>
                <div class="dm-message-time">${createdAt}</div>
            </div>
        </div>
    `;
}

function formatNumber(num) {
    if (num >= 1000000) {
        return (num / 1000000).toFixed(1) + 'M';
    }
    if (num >= 1000) {
        return (num / 1000).toFixed(1) + 'K';
    }
    return num.toString();
}

function formatTweetTime(dateString) {
    if (!dateString) return '';
    try {
        const date = new Date(dateString);
        const now = new Date();
        const diff = now - date;
        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);
        
        if (days > 0) return `${days}d`;
        if (hours > 0) return `${hours}h`;
        if (minutes > 0) return `${minutes}m`;
        return 'now';
    } catch (e) {
        return '';
    }
}

// Request Builder
function addQueryParam() {
    const container = document.getElementById('queryParamsContainer');
    const row = document.createElement('div');
    row.className = 'param-row';
    row.innerHTML = `
        <input type="text" placeholder="key (optional)" class="param-key text-input">
        <input type="text" placeholder="value" class="param-value text-input">
        <button class="btn-icon" onclick="removeParam(this)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"></line>
                <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
        </button>
    `;
    container.appendChild(row);
    // Add event listeners to new inputs
    const keyInput = row.querySelector('.param-key');
    const valueInput = row.querySelector('.param-value');
    if (keyInput) keyInput.addEventListener('input', updateRequestCommand);
    if (valueInput) valueInput.addEventListener('input', updateRequestCommand);
    updateRequestCommand();
}

function removeParam(btn) {
    btn.closest('.param-row').remove();
    updateRequestCommand();
}

document.getElementById('requestMethod').addEventListener('change', (e) => {
    const bodyGroup = document.getElementById('requestBodyGroup');
    const endpointSelect = document.getElementById('requestEndpoint');
    const selectedOption = endpointSelect ? endpointSelect.options[endpointSelect.selectedIndex] : null;
    const hasBody = selectedOption ? selectedOption.dataset.hasBody === 'true' : false;
    
    // Only show body field if method is POST/PUT/PATCH AND endpoint has a body
    if ((e.target.value === 'POST' || e.target.value === 'PUT' || e.target.value === 'PATCH') && hasBody) {
        bodyGroup.style.display = 'block';
    } else {
        bodyGroup.style.display = 'none';
    }
    
    // Filter endpoints by selected method
    filterEndpointsByMethod(e.target.value);
    
    // Update request command
    updateRequestCommand();
});

// Filter endpoints dropdown by method
function filterEndpointsByMethod(selectedMethod) {
    const endpointSelect = document.getElementById('requestEndpoint');
    if (!endpointSelect) {
        return; // Endpoint select doesn't exist yet
    }
    
    // First, show/hide all options based on method match
    for (let i = 0; i < endpointSelect.options.length; i++) {
        const option = endpointSelect.options[i];
        // Skip the placeholder option
        if (!option.value) {
            continue;
        }
        
        const optionMethod = option.dataset.method;
        if (optionMethod && optionMethod === selectedMethod) {
            option.style.display = '';
        } else if (optionMethod) {
            option.style.display = 'none';
        }
    }
    
    // Then, show/hide optgroups based on whether they have any visible options
    const optgroups = endpointSelect.querySelectorAll('optgroup');
    optgroups.forEach(optgroup => {
        let hasVisibleOption = false;
        // Use children or querySelectorAll instead of options property
        const options = Array.from(optgroup.children).filter(el => el.tagName === 'OPTION');
        for (let j = 0; j < options.length; j++) {
            const option = options[j];
            if (option.style.display !== 'none' && option.dataset.method === selectedMethod) {
                hasVisibleOption = true;
                break;
            }
        }
        if (hasVisibleOption) {
            optgroup.style.display = '';
        } else {
            optgroup.style.display = 'none';
        }
    });
    
    // If current selection doesn't match the method, clear it
    const selectedOption = endpointSelect.options[endpointSelect.selectedIndex];
    if (selectedOption && selectedOption.value && selectedOption.dataset.method !== selectedMethod) {
        endpointSelect.value = '';
        // Clear endpoint info
        const endpointInfo = document.getElementById('endpointInfo');
        if (endpointInfo) {
            endpointInfo.style.display = 'none';
        }
        currentEndpointInfo = null;
        // Clear query params
        const queryParamsContainer = document.getElementById('queryParamsContainer');
        if (queryParamsContainer) {
            queryParamsContainer.innerHTML = `
                <div class="param-row">
                    <input type="text" placeholder="key" class="param-key text-input">
                    <input type="text" placeholder="value" class="param-value text-input">
                    <button class="btn-icon" onclick="removeParam(this)">
                        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <line x1="18" y1="6" x2="6" y2="18"></line>
                            <line x1="6" y1="6" x2="18" y2="18"></line>
                        </svg>
                    </button>
                </div>
            `;
        }
    }
}

// Load available endpoints
async function loadEndpoints() {
    try {
        const response = await fetch(`${API_BASE_URL}/endpoints`);
        
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const data = await response.json();
        
        if (!data || !Array.isArray(data.endpoints)) {
            throw new Error('Invalid response format: endpoints array not found');
        }
        
        endpointsCache = data.endpoints || [];
        
        if (endpointsCache.length === 0) {
            console.warn('No endpoints found in response');
        }
        
        const endpointSelect = document.getElementById('requestEndpoint');
        
        // Show all endpoints together, marking streaming ones
        const grouped = {};
        endpointsCache.forEach(ep => {
            // Exclude rule management endpoints - these are NOT streaming endpoints
            const isRuleManagementEndpoint = ep.path.includes('/stream/rules');
            
            // Check if streaming, but exclude rule management endpoints
            const isStreaming = !isRuleManagementEndpoint && (
                ep.path.includes('/stream') || 
                (ep.tags && ep.tags.some(tag => tag.toLowerCase().includes('stream')))
            );
            const tag = ep.tags && ep.tags.length > 0 ? ep.tags[0] : 'Other';
            if (!grouped[tag]) {
                grouped[tag] = [];
            }
            ep.isStreaming = isStreaming; // Mark for later use
            grouped[tag].push(ep);
        });
        
        // Add grouped options to Request Builder
        Object.keys(grouped).sort().forEach(tag => {
            const optgroup = document.createElement('optgroup');
            optgroup.label = tag;
            grouped[tag].forEach(ep => {
                const option = document.createElement('option');
                option.value = ep.path;
                option.textContent = `${ep.method} ${ep.path}`;
                option.dataset.method = ep.method;
                option.dataset.summary = ep.summary || '';
                option.dataset.description = ep.description || '';
                option.dataset.parameters = JSON.stringify(ep.parameters || []);
                option.dataset.hasBody = ep.has_body ? 'true' : 'false';
                option.dataset.security = JSON.stringify(ep.security || []);
                option.dataset.isStreaming = ep.isStreaming ? 'true' : 'false';
                if (ep.request_body_schema) {
                    option.dataset.requestBodyExample = JSON.stringify(ep.request_body_schema);
                }
                optgroup.appendChild(option);
            });
            endpointSelect.appendChild(optgroup);
        });
        
        // Apply initial filter based on selected method (only if endpoints were loaded successfully)
        if (endpointsCache.length > 0) {
            const selectedMethod = document.getElementById('requestMethod').value;
            if (selectedMethod) {
                filterEndpointsByMethod(selectedMethod);
            }
        }
        
        // Set up event listeners for request command updates
        setupRequestCommandListeners();
    } catch (error) {
        console.error('Failed to load endpoints:', error);
        console.error('Error details:', {
            message: error.message,
            stack: error.stack,
            apiBaseUrl: API_BASE_URL
        });
        
        // Fallback to manual input with more helpful message
        const endpointSelect = document.getElementById('requestEndpoint');
        const errorMessage = error.message || 'Unknown error';
        endpointSelect.innerHTML = `<option value="">Failed to load endpoints (${errorMessage}) - you can type manually</option>`;
        
        // Also show an alert or console message for debugging
        console.warn('Unable to load endpoints from server. Make sure the server is running and accessible at:', API_BASE_URL);
    }
}

function onEndpointSelected() {
    // This function is called from HTML onchange
    selectEndpoint();
    
    // Auto-detect if this is a streaming endpoint
    const endpointSelectEl = document.getElementById('requestEndpoint');
    const selectedOption = endpointSelectEl.options[endpointSelectEl.selectedIndex];
    const isStreaming = selectedOption?.dataset.isStreaming === 'true';
    
    // Update UI for streaming vs regular endpoint
    updateUIForEndpoint(isStreaming);
    updateAuthRequirements('request');
}

function selectEndpoint() {
    const endpointSelect = document.getElementById('requestEndpoint');
    const selectedOption = endpointSelect.options[endpointSelect.selectedIndex];
    
    if (!selectedOption || !selectedOption.value) {
        document.getElementById('endpointInfo').style.display = 'none';
        currentEndpointInfo = null;
        // Clear query params
        const paramsContainer = document.getElementById('queryParamsContainer');
        paramsContainer.innerHTML = `
            <div class="param-row">
                <input type="text" placeholder="key" class="param-key text-input">
                <input type="text" placeholder="value" class="param-value text-input">
                <button class="btn-icon" onclick="removeParam(this)">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="18" y1="6" x2="6" y2="18"></line>
                        <line x1="6" y1="6" x2="18" y2="18"></line>
                    </svg>
                </button>
            </div>
        `;
        // Add event listeners
        const row = paramsContainer.querySelector('.param-row');
        const keyInput = row.querySelector('.param-key');
        const valueInput = row.querySelector('.param-value');
        if (keyInput) keyInput.addEventListener('input', updateRequestCommand);
        if (valueInput) valueInput.addEventListener('input', updateRequestCommand);
        updateRequestCommand();
        return;
    }
    
    const method = selectedOption.dataset.method;
    const summary = selectedOption.dataset.summary;
    const description = selectedOption.dataset.description;
    const parameters = JSON.parse(selectedOption.dataset.parameters || '[]');
    const hasBody = selectedOption.dataset.hasBody === 'true';
    const requestBodyExample = selectedOption.dataset.requestBodyExample;
    
    // Update method
    document.getElementById('requestMethod').value = method;
    
    // Update method change handler to show/hide body
    const bodyGroup = document.getElementById('requestBodyGroup');
    const bodyTextarea = document.getElementById('requestBody');
    // Show request body only if the endpoint actually has a body schema with properties
    // For endpoints like /dm/block and /dm/unblock that don't need a body, hide the field
    if ((method === 'POST' || method === 'PUT' || method === 'PATCH') && hasBody) {
        bodyGroup.style.display = 'block';
        // Populate request body with example if available
        if (requestBodyExample) {
            try {
                const example = JSON.parse(requestBodyExample);
                bodyTextarea.value = JSON.stringify(example, null, 2);
            } catch (e) {
                console.error('Failed to parse request body example:', e);
                bodyTextarea.value = '';
            }
        } else {
            // Don't clear existing body if user has already entered something
            // Only clear if it's empty or if we're switching endpoints
            if (!bodyTextarea.value.trim()) {
                bodyTextarea.value = '';
            }
        }
    } else {
        bodyGroup.style.display = 'none';
        bodyTextarea.value = '';
    }
    
    // Update request command
    updateRequestCommand();
    
    // Show endpoint info
    const infoDiv = document.getElementById('endpointInfo');
    const summaryDiv = document.getElementById('endpointSummary');
    const descDiv = document.getElementById('endpointDescription');
    const paramsDiv = document.getElementById('endpointParams');
    
    summaryDiv.textContent = summary || `${method} ${selectedOption.value}`;
    descDiv.textContent = description || '';
    
    // Show parameters
    const queryParams = parameters.filter(p => p.in === 'query');
    const pathParams = parameters.filter(p => p.in === 'path');
    
    let paramsHtml = '';
    if (pathParams.length > 0) {
        paramsHtml += '<div style="margin-bottom: 8px;"><strong>Path Parameters:</strong> ';
        paramsHtml += pathParams.map(p => `<span style="color: var(--primary);">${escapeHtml(p.name)}</span>`).join(', ');
        paramsHtml += '</div>';
    }
    
    if (queryParams.length > 0) {
        paramsHtml += '<div><strong>Query Parameters:</strong><ul style="margin: 8px 0 0 20px; padding: 0;">';
        queryParams.forEach(p => {
            paramsHtml += `<li style="margin-bottom: 4px;">
                <span style="color: var(--primary); font-weight: 600;">${escapeHtml(p.name)}</span>
                ${p.required ? '<span style="color: var(--danger); font-size: 11px;">(required)</span>' : ''}
                ${p.description ? ` - <span style="color: var(--text-muted);">${escapeHtml(p.description)}</span>` : ''}
            </li>`;
        });
        paramsHtml += '</ul></div>';
    } else {
        paramsHtml += '<div style="color: var(--text-muted);">No query parameters</div>';
    }
    
    paramsDiv.innerHTML = paramsHtml;
    infoDiv.style.display = 'block';
    
    // Get security info from option dataset (needed for currentEndpointInfo)
    const securityJson = selectedOption.dataset.security || '[]';
    let security = [];
    try {
        security = JSON.parse(securityJson);
    } catch (e) {
        security = [];
    }
    
    // Store current endpoint info BEFORE creating parameter rows so dropdowns can access it
    currentEndpointInfo = {
        method: method,
        path: selectedOption.value,
        parameters: parameters,
        hasBody: hasBody,
        security: security
    };
    
    // Pre-populate params container with path and query parameters
    const paramsContainer = document.getElementById('queryParamsContainer');
    paramsContainer.innerHTML = '';
    
    // Add path parameters first
    if (pathParams.length > 0) {
        pathParams.forEach(param => {
            const row = document.createElement('div');
            row.className = 'param-row';
            row.innerHTML = `
                <input type="text" placeholder="${escapeHtml(param.name)}${param.required ? ' *' : ''}" 
                       class="param-key text-input" value="${escapeHtml(param.name)}" readonly 
                       style="background: var(--bg-tertiary); cursor: not-allowed; color: var(--primary); font-weight: 600;">
                <input type="text" placeholder="${param.description ? escapeHtml(param.description.substring(0, 50)) : 'value (required for path)'}" 
                       class="param-value text-input" ${param.required ? 'required' : ''}>
                <span style="color: var(--text-muted); font-size: 12px; padding: 0 8px; display: flex; align-items: center;">path</span>
                <button class="btn-icon" onclick="removeParam(this)" style="display: none;">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="18" y1="6" x2="6" y2="18"></line>
                        <line x1="6" y1="6" x2="18" y2="18"></line>
                    </svg>
                </button>
            `;
            paramsContainer.appendChild(row);
            // Add event listener to value input
            const valueInput = row.querySelector('.param-value');
            if (valueInput) {
                valueInput.addEventListener('input', updateRequestCommand);
            }
        });
    }
    
    // Add query parameters
    if (queryParams.length > 0) {
        queryParams.forEach(param => {
            const row = document.createElement('div');
            row.className = 'param-row';
            row.innerHTML = `
                <input type="text" placeholder="${escapeHtml(param.name)}${param.required ? ' *' : ''}" 
                       class="param-key text-input" value="${escapeHtml(param.name)}" readonly 
                       style="background: var(--bg-tertiary); cursor: not-allowed;">
                <input type="text" placeholder="${param.description ? escapeHtml(param.description.substring(0, 50)) : 'value'}" 
                       class="param-value text-input" ${param.required ? 'required' : ''}>
                <button class="btn-icon" onclick="removeParam(this)">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="18" y1="6" x2="6" y2="18"></line>
                        <line x1="6" y1="6" x2="18" y2="18"></line>
                    </svg>
                </button>
            `;
            paramsContainer.appendChild(row);
            // Add event listener to value input
            const valueInput = row.querySelector('.param-value');
            if (valueInput) {
                valueInput.addEventListener('input', updateRequestCommand);
            }
        });
    }
    
    // Add a default empty row if no params
    if (pathParams.length === 0 && queryParams.length === 0) {
        const row = document.createElement('div');
        row.className = 'param-row';
        row.innerHTML = `
            <input type="text" placeholder="key" class="param-key text-input">
            <input type="text" placeholder="value" class="param-value text-input">
            <button class="btn-icon" onclick="removeParam(this)">
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="18" y1="6" x2="6" y2="18"></line>
                    <line x1="6" y1="6" x2="18" y2="18"></line>
                </svg>
            </button>
        `;
        paramsContainer.appendChild(row);
        const keyInput = row.querySelector('.param-key');
        const valueInput = row.querySelector('.param-value');
        if (keyInput) keyInput.addEventListener('input', updateRequestCommand);
        if (valueInput) valueInput.addEventListener('input', updateRequestCommand);
    }
    
    // Update request command after populating params
    updateRequestCommand();
    
    // Always add one empty row for custom parameters
    const row = document.createElement('div');
    row.className = 'param-row';
    row.innerHTML = `
        <input type="text" placeholder="key (optional)" class="param-key text-input">
        <input type="text" placeholder="value" class="param-value text-input">
        <button class="btn-icon" onclick="removeParam(this)">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"></line>
                <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
        </button>
    `;
    paramsContainer.appendChild(row);
    
    // Add event listeners
    const keyInput = row.querySelector('.param-key');
    const valueInput = row.querySelector('.param-value');
    if (keyInput) keyInput.addEventListener('input', updateRequestCommand);
    if (valueInput) valueInput.addEventListener('input', updateRequestCommand);
    
    // Update auth requirements for this endpoint
    updateAuthRequirements('request');
}

function getAuthHeader(authType) {
    const prefix = authType === 'request' ? 'request' : 'stream';
    const method = document.getElementById(`${prefix}AuthMethod`).value;
    
    if (method === 'none') {
        return '';
    }
    
    if (method === 'bearer') {
        const token = document.getElementById(`${prefix}Auth`).value.trim();
        if (!token) return '';
        // If token doesn't start with "Bearer ", add it
        return token.startsWith('Bearer ') ? token : `Bearer ${token}`;
    }
    
    if (method === 'oauth1') {
        // OAuth 1.0a format: OAuth oauth_consumer_key="...", oauth_token="...", etc.
        const consumerKey = document.getElementById(`${prefix}AuthConsumerKey`)?.value.trim() || '';
        const consumerSecret = document.getElementById(`${prefix}AuthConsumerSecret`)?.value.trim() || '';
        const token = document.getElementById(`${prefix}AuthToken`)?.value.trim() || '';
        const tokenSecret = document.getElementById(`${prefix}AuthTokenSecret`)?.value.trim() || '';
        
        if (!consumerKey || !token) return '';
        
        // Build OAuth 1.0a header
        const params = [
            `oauth_consumer_key="${consumerKey}"`,
            `oauth_token="${token}"`,
            'oauth_signature_method="HMAC-SHA1"',
            `oauth_timestamp="${Math.floor(Date.now() / 1000)}"`,
            `oauth_nonce="${Math.random().toString(36).substring(2, 15)}"`,
            'oauth_version="1.0"'
        ];
        
        // Note: In a real implementation, you'd need to generate the signature
        // For the playground, we'll use a placeholder signature
        params.push('oauth_signature="playground_signature"');
        
        return `OAuth ${params.join(', ')}`;
    }
    
    if (method === 'oauth2') {
        // OAuth 2.0 User Context uses Bearer token format but with user context token
        const token = document.getElementById(`${prefix}Auth`).value.trim();
        if (!token) return '';
        return token.startsWith('Bearer ') ? token : `Bearer ${token}`;
    }
    
    return '';
}

function updateAuthMethod(authType) {
    // Use 'request' for both since we merged the views
    const prefix = 'request';
    const method = document.getElementById(`${prefix}AuthMethod`).value;
    const fieldsContainer = document.getElementById(`${prefix}AuthFields`);
    const infoContainer = document.getElementById(`${prefix}AuthInfo`);
    
    if (!fieldsContainer) return;
    
    let html = '';
    let info = '';
    
    if (method === 'bearer') {
        html = `
            <input type="text" id="${prefix}Auth" class="text-input" placeholder="Bearer token or just token" value="Bearer test" oninput="updateRequestCommand()">
            <div style="font-size: 11px; color: var(--text-muted); margin-top: 4px;">
                Enter your Bearer token. Include "Bearer " prefix or just the token.
            </div>
        `;
        info = 'OAuth 2.0 Application-Only authentication. Used for read-only endpoints that don\'t require user context.';
    } else if (method === 'oauth1') {
        html = `
            <div style="display: flex; flex-direction: column; gap: 8px;">
                <div>
                    <label style="font-size: 11px; color: var(--text-secondary); margin-bottom: 4px; display: block;">Consumer Key</label>
                    <input type="text" id="${prefix}AuthConsumerKey" class="text-input" placeholder="Consumer Key" value="your_consumer_key_here" oninput="updateRequestCommand()">
                </div>
                <div>
                    <label style="font-size: 11px; color: var(--text-secondary); margin-bottom: 4px; display: block;">Consumer Secret</label>
                    <input type="password" id="${prefix}AuthConsumerSecret" class="text-input" placeholder="Consumer Secret" value="your_consumer_secret_here" oninput="updateRequestCommand()">
                </div>
                <div>
                    <label style="font-size: 11px; color: var(--text-secondary); margin-bottom: 4px; display: block;">Access Token</label>
                    <input type="text" id="${prefix}AuthToken" class="text-input" placeholder="Access Token" value="your_access_token_here" oninput="updateRequestCommand()">
                </div>
                <div>
                    <label style="font-size: 11px; color: var(--text-secondary); margin-bottom: 4px; display: block;">Access Token Secret</label>
                    <input type="password" id="${prefix}AuthTokenSecret" class="text-input" placeholder="Access Token Secret" value="your_access_token_secret_here" oninput="updateRequestCommand()">
                </div>
            </div>
            <div style="font-size: 11px; color: var(--text-muted); margin-top: 4px;">
                Note: OAuth 1.0a signature generation is simulated in the playground. Real API requires proper signature.
            </div>
        `;
        info = 'OAuth 1.0a User Context authentication. Required for endpoints that need user context (tweets, follows, etc.).';
    } else if (method === 'oauth2') {
        html = `
            <input type="text" id="${prefix}Auth" class="text-input" placeholder="OAuth 2.0 User Access Token" value="Bearer test" oninput="updateRequestCommand()">
            <div style="font-size: 11px; color: var(--text-muted); margin-top: 4px;">
                Enter your OAuth 2.0 User Access Token. Include "Bearer " prefix or just the token.
            </div>
        `;
        info = 'OAuth 2.0 User Context authentication. Required for endpoints that need user context (tweets, follows, etc.).';
    } else {
        html = '<div style="font-size: 12px; color: var(--text-muted); padding: 8px;">No authentication will be sent with this request.</div>';
        info = 'No authentication. Only works if "Disable Authentication Validation" is enabled in Settings.';
    }
    
    fieldsContainer.innerHTML = html;
    if (infoContainer) {
        infoContainer.textContent = info;
    }
    
    // Update endpoint auth requirements if endpoint is selected
    updateAuthRequirements(authType);
    updateRequestCommand();
}

function updateAuthRequirements(authType) {
    const prefix = authType === 'request' ? 'request' : 'stream';
    const endpointSelect = document.getElementById(`${prefix}Endpoint`);
    const infoContainer = document.getElementById(`${prefix}AuthInfo`);
    const authMethodSelect = document.getElementById(`${prefix}AuthMethod`);
    
    if (!endpointSelect || !endpointSelect.value || !infoContainer || !authMethodSelect) return;
    
    // Get endpoint info from selected option dataset
    const selectedOption = endpointSelect.options[endpointSelect.selectedIndex];
    if (!selectedOption) return;
    
    // Get security from dataset (stored when endpoints were loaded)
    const securityJson = selectedOption.dataset.security || '[]';
    let securityMethods = [];
    try {
        securityMethods = JSON.parse(securityJson);
    } catch (e) {
        securityMethods = [];
    }
    
    // Map OpenAPI security scheme names to our auth method values
    const supportedMethods = [];
    if (securityMethods.length === 0) {
        // No security requirements = accept any auth (or none)
        supportedMethods.push('bearer', 'oauth1', 'oauth2', 'none');
    } else {
        // Only show auth methods that match the OpenAPI spec
        if (securityMethods.includes('BearerToken')) {
            supportedMethods.push('bearer');
        }
        if (securityMethods.includes('UserToken')) {
            supportedMethods.push('oauth1');
        }
        if (securityMethods.includes('OAuth2UserToken')) {
            supportedMethods.push('oauth2');
        }
        // If no security methods match, allow none (for endpoints that don't require auth)
        if (supportedMethods.length === 0) {
            supportedMethods.push('none');
        }
    }
    
    // Filter auth method dropdown options
    const currentValue = authMethodSelect.value;
    authMethodSelect.innerHTML = '';
    
    const options = [
        { value: 'bearer', label: 'Bearer Token (OAuth 2.0 App-Only)' },
        { value: 'oauth1', label: 'OAuth 1.0a User Context' },
        { value: 'oauth2', label: 'OAuth 2.0 User Context' },
        { value: 'none', label: 'No Authentication' }
    ];
    
    options.forEach(opt => {
        if (supportedMethods.includes(opt.value)) {
            const option = document.createElement('option');
            option.value = opt.value;
            option.textContent = opt.label;
            authMethodSelect.appendChild(option);
        }
    });
    
    // Restore previous selection if still valid, otherwise select first available
    if (supportedMethods.includes(currentValue)) {
        authMethodSelect.value = currentValue;
    } else if (supportedMethods.length > 0) {
        authMethodSelect.value = supportedMethods[0];
        updateAuthMethod(authType);
    }
    
    // Update info message
    if (securityMethods.length === 0) {
        infoContainer.textContent = 'This endpoint accepts any authentication method or no authentication.';
    } else {
        const methodNames = securityMethods.map(s => {
            if (s === 'BearerToken') return 'Bearer Token (OAuth 2.0 App-Only)';
            if (s === 'UserToken') return 'OAuth 1.0a User Context';
            if (s === 'OAuth2UserToken') return 'OAuth 2.0 User Context';
            return s;
        }).join(', ');
        infoContainer.textContent = `This endpoint requires: ${methodNames}`;
    }
}

// Auto-detect streaming endpoint and update UI accordingly
function updateUIForEndpoint(isStreaming) {
    const sendBtn = document.getElementById('sendRequestBtn');
    const sendBtnText = document.getElementById('sendRequestBtnText');
    const responseTitle = document.getElementById('responseHeaderTitle');
    const responseContent = document.getElementById('responseContent');
    const streamContent = document.getElementById('streamContent');
    const clearResponseBtn = document.getElementById('clearResponseBtn');
    
    if (isStreaming) {
        sendBtnText.textContent = 'Start Stream';
        sendBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polygon points="5 3 19 12 5 21 5 3"></polygon></svg><span id="sendRequestBtnText">Start Stream</span>';
        responseTitle.textContent = 'Stream Output';
        responseContent.style.display = 'none';
        streamContent.style.display = 'block';
        clearResponseBtn.style.display = 'inline-flex';
    } else {
        sendBtnText.textContent = 'Send Request';
        sendBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="22" y1="2" x2="11" y2="13"></line><polygon points="22 2 15 22 11 13 2 9 22 2"></polygon></svg><span id="sendRequestBtnText">Send Request</span>';
        responseTitle.textContent = 'Response';
        responseContent.style.display = 'block';
        streamContent.style.display = 'none';
        clearResponseBtn.style.display = 'none';
    }
}

function clearResponse() {
    // Check if currently showing stream content
    const streamContent = document.getElementById('streamContent');
    if (streamContent && streamContent.style.display !== 'none') {
        clearStreamOutput();
    } else {
        document.getElementById('responseContent').innerHTML = '<div class="empty-state">Send a request to see the response here</div>';
        document.getElementById('responseStatus').textContent = '-';
    }
}

async function sendRequest() {
    // Auto-detect if this is a streaming endpoint
    const endpointSelect = document.getElementById('requestEndpoint');
    const selectedOption = endpointSelect.options[endpointSelect.selectedIndex];
    const isStreaming = selectedOption?.dataset.isStreaming === 'true';
    
    // If streaming endpoint, use startStream instead
    if (isStreaming) {
        return startStream();
    }
    
    const method = document.getElementById('requestMethod').value;
    const endpoint = endpointSelect.value.trim();
    const auth = getAuthHeader('request');
    const body = document.getElementById('requestBody').value.trim();
    
    if (!endpoint) {
        showToast('Please select an endpoint', 'warning');
        return;
    }
    
    try {
        // Replace path parameters if needed
        let finalEndpoint = endpoint;
        const pathParamNames = new Set();
        
        if (currentEndpointInfo) {
            const pathParams = currentEndpointInfo.parameters.filter(p => p.in === 'path');
            pathParams.forEach(param => {
                pathParamNames.add(param.name);
                // Look for the param value in the param rows
                document.querySelectorAll('.param-row').forEach(row => {
                    const keyInput = row.querySelector('.param-key');
                    const valueInput = row.querySelector('.param-value');
                    if (keyInput && valueInput && keyInput.value.trim() === param.name) {
                        const value = valueInput.value.trim();
                        if (value) {
                            finalEndpoint = finalEndpoint.replace(`{${param.name}}`, encodeURIComponent(value));
                        } else if (param.required) {
                            showToast(`Path parameter '${param.name}' is required`, 'error');
                            throw new Error(`Path parameter '${param.name}' is required`);
                        }
                    }
                });
            });
        }
        
        // Build query parameters (excluding path params)
        const params = [];
        document.querySelectorAll('.param-row').forEach(row => {
            const keyInput = row.querySelector('.param-key');
            const valueInput = row.querySelector('.param-value');
            if (!keyInput || !valueInput) return;
            
            const key = keyInput.value.trim();
            const value = valueInput.value.trim();
            
            // Skip path parameters (they're already in the URL)
            if (pathParamNames.has(key)) {
                return;
            }
            
            if (key && value) {
                params.push(`${encodeURIComponent(key)}=${encodeURIComponent(value)}`);
            }
        });
        const queryString = params.length > 0 ? '?' + params.join('&') : '';
        
        const url = `${API_BASE_URL}${finalEndpoint}${queryString}`;
        const options = {
            method: method,
            headers: {
                'Content-Type': 'application/json',
            }
        };
        
        if (auth) {
            options.headers['Authorization'] = auth;
            // Add X-Auth-Method header to help backend distinguish between Bearer Token (App-Only) 
            // and OAuth 2.0 User Context (both use "Bearer" format)
            const authMethod = document.getElementById('requestAuthMethod').value;
            if (authMethod === 'bearer') {
                options.headers['X-Auth-Method'] = 'BearerToken';
            } else if (authMethod === 'oauth1') {
                options.headers['X-Auth-Method'] = 'OAuth1a';
            } else if (authMethod === 'oauth2') {
                options.headers['X-Auth-Method'] = 'OAuth2User';
            }
        } else {
            // Remove Authorization header if no auth
            delete options.headers['Authorization'];
        }
        
        if (body && (method === 'POST' || method === 'PUT' || method === 'PATCH')) {
            try {
                options.body = JSON.stringify(JSON.parse(body));
            } catch (e) {
                showToast('Invalid JSON in request body', 'error');
                return;
            }
        }
        
        const startTime = Date.now();
        const responseContent = document.getElementById('responseContent');
        const responseStatus = document.getElementById('responseStatus');
        
        responseContent.innerHTML = '<div class="loading">Sending request...</div>';
        responseStatus.textContent = '-';
        
        const response = await fetch(url, options);
        const responseTime = Date.now() - startTime;
        
        let responseData;
        const contentType = response.headers.get('content-type');
        if (contentType && contentType.includes('application/json')) {
            responseData = await response.json();
        } else {
            responseData = await response.text();
        }
        
        // Update status badge
        responseStatus.textContent = `${response.status} ${response.statusText}`;
        responseStatus.className = `status-badge ${response.status >= 200 && response.status < 300 ? 'success' : 'error'}`;
        
        // Display response
        if (typeof responseData === 'string') {
            responseContent.innerHTML = `<pre class="json-viewer">${escapeHtml(responseData)}</pre>`;
        } else {
            responseContent.innerHTML = `<pre class="json-viewer">${syntaxHighlight(JSON.stringify(responseData, null, 2))}</pre>`;
        }
        
        // Parse request body if it's JSON
        let requestBodyParsed = null;
        if (body) {
            try {
                requestBodyParsed = JSON.parse(body);
            } catch (e) {
                requestBodyParsed = body; // Keep as string if not valid JSON
            }
        }
        
        // Add to history with full details
        addToHistory({
            method,
            endpoint: finalEndpoint,
            fullUrl: url,
            status: response.status,
            statusText: response.statusText,
            responseTime,
            timestamp: new Date().toISOString(),
            requestHeaders: options.headers,
            requestBody: requestBodyParsed,
            queryParams: params,
            responseHeaders: Object.fromEntries(response.headers.entries()),
            responseBody: responseData,
            responseSize: typeof responseData === 'string' ? responseData.length : JSON.stringify(responseData).length
        });
        
    } catch (error) {
        if (error.message && error.message.includes('Path parameter')) {
            return; // Already showed alert
        }
        const responseStatus = document.getElementById('responseStatus');
        const responseContent = document.getElementById('responseContent');
        responseStatus.textContent = 'Error';
        responseStatus.className = 'status-badge error';
        responseContent.innerHTML = `<div style="color: var(--danger);">Request failed: ${error.message}</div>`;
        
        addToHistory({
            method,
            endpoint: endpoint,
            status: 0,
            error: error.message,
            timestamp: new Date().toISOString(),
            requestHeaders: {},
            requestBody: body || null,
            queryParams: []
        });
    }
}

function addToHistory(item) {
    requestHistory.unshift(item);
    if (requestHistory.length > 100) {
        requestHistory = requestHistory.slice(0, 100);
    }
    localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
    updateRequestHistory();
}

function updateRequestHistory() {
    const historyList = document.getElementById('requestHistory');
    if (requestHistory.length === 0) {
        historyList.innerHTML = '<div class="empty-state">No requests yet</div>';
        return;
    }
    
    historyList.innerHTML = requestHistory.map((item, index) => {
        const isStream = item.isStream || false;
        const streamIndicator = isStream ? ' 🔄' : '';
        const statusDisplay = isStream 
            ? (item.streamStatus === 'ended' ? 'Ended' : item.streamStatus === 'stopped' ? 'Stopped' : item.streamStatus === 'error' ? 'Error' : 'Streaming')
            : (item.status > 0 ? item.status : 'Error');
        const statusClass = isStream
            ? (item.streamStatus === 'ended' ? 'success' : item.streamStatus === 'error' ? 'error' : 'warning')
            : (item.status >= 200 && item.status < 300 ? 'success' : item.status === 0 ? 'error' : 'warning');
        const metaText = isStream && item.messageCount > 0 
            ? `${item.messageCount} msg${item.messageCount !== 1 ? 's' : ''}`
            : '';
        
        return `
        <div class="history-item-container">
            <div class="history-item" onclick="toggleHistoryItem(${index})">
                <span class="history-item-method ${item.method}">${item.method}</span>
                <span class="history-item-endpoint">${escapeHtml(item.endpoint)}${streamIndicator}</span>
                <span class="history-item-status ${statusClass}">${statusDisplay}</span>
                ${metaText ? `<span class="history-item-time" style="font-size: 11px; color: var(--text-muted);">${escapeHtml(metaText)}</span>` : ''}
                <span class="history-item-time">${formatTime(item.timestamp)}</span>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="history-toggle-icon" id="history-toggle-${index}">
                    <polyline points="6 9 12 15 18 9"></polyline>
                </svg>
            </div>
            <div class="history-item-details" id="history-details-${index}" style="display: none;">
                ${renderHistoryDetails(item)}
            </div>
        </div>
        `;
    }).join('');
}

function toggleHistoryItem(index) {
    const detailsDiv = document.getElementById(`history-details-${index}`);
    const toggleIcon = document.getElementById(`history-toggle-${index}`);
    
    if (detailsDiv.style.display === 'none') {
        detailsDiv.style.display = 'block';
        toggleIcon.style.transform = 'rotate(180deg)';
    } else {
        detailsDiv.style.display = 'none';
        toggleIcon.style.transform = 'rotate(0deg)';
    }
}

function renderHistoryDetails(item) {
    const isStream = item.isStream || false;
    const queryString = item.queryParams && item.queryParams.length > 0 
        ? '?' + item.queryParams.map(p => `${escapeHtml(p.split('=')[0])}=${escapeHtml(p.split('=')[1] || '')}`).join('&')
        : '';
    
    return `
        <div class="history-details-content">
            <div class="history-detail-section">
                <h4>${isStream ? 'Stream' : 'Request'} Configuration</h4>
                <div class="history-detail-row">
                    <strong>URL:</strong>
                    <code class="history-code">${escapeHtml(item.fullUrl || item.endpoint + queryString)}</code>
                </div>
                <div class="history-detail-row">
                    <strong>Method:</strong>
                    <span>${escapeHtml(item.method)}</span>
                </div>
                ${item.requestHeaders && Object.keys(item.requestHeaders).length > 0 ? `
                    <div class="history-detail-row">
                        <strong>Headers:</strong>
                        <pre class="history-code-block">${escapeHtml(JSON.stringify(item.requestHeaders, null, 2))}</pre>
                    </div>
                ` : ''}
                ${item.requestBody !== null && item.requestBody !== undefined ? `
                    <div class="history-detail-row">
                        <strong>Body:</strong>
                        <pre class="history-code-block">${typeof item.requestBody === 'string' ? escapeHtml(item.requestBody) : syntaxHighlight(JSON.stringify(item.requestBody, null, 2))}</pre>
                    </div>
                ` : ''}
            </div>
            <div class="history-detail-section">
                <h4>${isStream ? 'Stream Statistics' : 'Response'}</h4>
                <div class="history-detail-row">
                    <strong>Status:</strong>
                    <span class="status-badge ${isStream 
                        ? (item.streamStatus === 'ended' ? 'success' : item.streamStatus === 'error' ? 'error' : 'warning')
                        : (item.status >= 200 && item.status < 300 ? 'success' : item.status === 0 ? 'error' : 'warning')}">
                        ${isStream 
                            ? (item.streamStatus === 'ended' ? 'Ended' : item.streamStatus === 'stopped' ? 'Stopped' : item.streamStatus === 'error' ? 'Error' : 'Streaming')
                            : (item.status > 0 ? `${item.status} ${item.statusText || ''}` : 'Error')}
                    </span>
                </div>
                ${isStream ? `
                    ${item.messageCount > 0 ? `
                        <div class="history-detail-row">
                            <strong>Messages Received:</strong>
                            <span>${item.messageCount}</span>
                        </div>
                    ` : ''}
                    ${item.duration > 0 ? `
                        <div class="history-detail-row">
                            <strong>Duration:</strong>
                            <span>${formatDuration(item.duration)}</span>
                        </div>
                    ` : ''}
                    <div class="history-detail-row">
                        <strong>Started:</strong>
                        <span>${new Date(item.timestamp).toLocaleString()}</span>
                    </div>
                    ${item.endTime ? `
                        <div class="history-detail-row">
                            <strong>Ended:</strong>
                            <span>${new Date(item.endTime).toLocaleString()}</span>
                        </div>
                    ` : ''}
                ` : `
                    ${item.responseTime ? `
                        <div class="history-detail-row">
                            <strong>Response Time:</strong>
                            <span>${item.responseTime}ms</span>
                        </div>
                    ` : ''}
                    ${item.responseHeaders && Object.keys(item.responseHeaders).length > 0 ? `
                        <div class="history-detail-row">
                            <strong>Headers:</strong>
                            <pre class="history-code-block">${escapeHtml(JSON.stringify(item.responseHeaders, null, 2))}</pre>
                        </div>
                    ` : ''}
                    ${item.responseBody !== null && item.responseBody !== undefined ? `
                        <div class="history-detail-row">
                            <strong>Body:</strong>
                            <pre class="history-code-block">${typeof item.responseBody === 'string' ? escapeHtml(item.responseBody) : syntaxHighlight(JSON.stringify(item.responseBody, null, 2))}</pre>
                        </div>
                    ` : ''}
                `}
                ${item.error ? `
                    <div class="history-detail-row">
                        <strong>Error:</strong>
                        <span style="color: var(--danger);">${escapeHtml(item.error)}</span>
                    </div>
                ` : ''}
            </div>
            <div class="history-detail-actions">
                <button class="btn btn-secondary btn-small" onclick="loadHistoryItem(${requestHistory.indexOf(item)})">
                    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M1 4v6h6"></path>
                        <path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"></path>
                    </svg>
                    Reuse ${isStream ? 'Stream' : 'Request'}
                </button>
                ${!isStream && item.responseBody ? `
                    <button class="btn btn-secondary btn-small" onclick="copyHistoryItem(${requestHistory.indexOf(item)})">
                        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                            <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                            <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                        </svg>
                        Copy Response
                    </button>
                ` : ''}
            </div>
        </div>
    `;
}

function copyHistoryItem(index) {
    const item = requestHistory[index];
    if (item.responseBody) {
        const text = typeof item.responseBody === 'string' 
            ? item.responseBody 
            : JSON.stringify(item.responseBody, null, 2);
        navigator.clipboard.writeText(text).then(() => {
            showToast('Response copied to clipboard', 'success', 3000);
        });
    }
}

function loadHistoryItem(index) {
    const item = requestHistory[index];
    const isStream = item.isStream || false;
    
    // Set method
    document.getElementById('requestMethod').value = item.method;
    
    // Find and select the endpoint in dropdown
    const endpointSelect = document.getElementById('requestEndpoint');
    for (let i = 0; i < endpointSelect.options.length; i++) {
        const option = endpointSelect.options[i];
        if (option.value === item.endpoint || option.value.endsWith(item.endpoint.split('?')[0])) {
            endpointSelect.selectedIndex = i;
            onEndpointSelected();
            break;
        }
    }
    
    // If endpoint not found in dropdown, set it directly (fallback)
    if (endpointSelect.value !== item.endpoint) {
        endpointSelect.value = item.endpoint;
        // Trigger UI update for streaming if needed
        const selectedOption = endpointSelect.options[endpointSelect.selectedIndex];
        if (selectedOption) {
            const isStreamingEndpoint = selectedOption.dataset.isStreaming === 'true';
            updateUIForEndpoint(isStreamingEndpoint);
        }
    } else {
        // Ensure UI is updated for streaming endpoints
        const selectedOption = endpointSelect.options[endpointSelect.selectedIndex];
        if (selectedOption && selectedOption.dataset.isStreaming === 'true') {
            updateUIForEndpoint(true);
        }
    }
    
    // Restore query parameters if available
    if (item.queryParams && item.queryParams.length > 0) {
        const paramsContainer = document.getElementById('queryParamsContainer');
        paramsContainer.innerHTML = '';
        item.queryParams.forEach(param => {
            const [key, value] = param.split('=');
            const row = document.createElement('div');
            row.className = 'param-row';
            row.innerHTML = `
                <input type="text" placeholder="key" class="param-key text-input" value="${escapeHtml(decodeURIComponent(key))}">
                <input type="text" placeholder="value" class="param-value text-input" value="${escapeHtml(decodeURIComponent(value || ''))}">
                <button class="btn-icon" onclick="removeParam(this)">
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                        <line x1="18" y1="6" x2="6" y2="18"></line>
                        <line x1="6" y1="6" x2="18" y2="18"></line>
                    </svg>
                </button>
            `;
            paramsContainer.appendChild(row);
        });
    }
    
    // Restore request body if available
    if (item.requestBody !== null && item.requestBody !== undefined) {
        const bodyText = typeof item.requestBody === 'string' 
            ? item.requestBody 
            : JSON.stringify(item.requestBody, null, 2);
        document.getElementById('requestBody').value = bodyText;
    }
    
    // Restore auth header if available
        if (item.requestHeaders && item.requestHeaders.Authorization) {
            const authHeader = item.requestHeaders.Authorization;
            // Detect auth method from header
            if (authHeader.includes('oauth_consumer_key') || authHeader.includes('oauth_signature')) {
                document.getElementById('requestAuthMethod').value = 'oauth1';
                updateAuthMethod('request');
                // Try to parse OAuth 1.0a params (simplified)
                // In a real implementation, you'd parse the full OAuth header
            } else if (authHeader.startsWith('Bearer ')) {
                document.getElementById('requestAuthMethod').value = 'bearer';
                updateAuthMethod('request');
                document.getElementById('requestAuth').value = authHeader;
            } else {
                document.getElementById('requestAuthMethod').value = 'bearer';
                updateAuthMethod('request');
                document.getElementById('requestAuth').value = authHeader;
            }
        }
    
    // Show Request Builder section
    showSection('api-builder');
    document.querySelectorAll('.nav-item').forEach(ni => {
        if (ni.dataset.section === 'api-builder') {
            ni.classList.add('active');
        } else {
            ni.classList.remove('active');
        }
    });
}

function clearRequestHistory() {
    showConfirm('Clear Request History', 'Are you sure you want to clear all request history?').then(confirmed => {
        if (confirmed) {
            requestHistory = [];
            localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
            updateRequestHistory();
        }
    });
}

function copyResponse() {
    const content = document.getElementById('responseContent').textContent;
    navigator.clipboard.writeText(content).then(() => {
        showToast('Response copied to clipboard', 'success', 3000);
    });
}

// Generate curl command from current form state
function generateRequestCommand() {
    const methodEl = document.getElementById('requestMethod');
    const method = methodEl ? methodEl.value : 'GET';
    const endpointSelect = document.getElementById('requestEndpoint');
    const endpoint = endpointSelect ? endpointSelect.value.trim() : '';
    const authEl = document.getElementById('requestAuth');
    const auth = authEl ? authEl.value.trim() : '';
    const bodyEl = document.getElementById('requestBody');
    const body = bodyEl ? bodyEl.value.trim() : '';
    const commandTypeEl = document.getElementById('requestCommandType');
    const commandType = commandTypeEl ? commandTypeEl.value : 'curl';
    
    if (!endpoint) {
        return null;
    }
    
    try {
        // Replace path parameters if needed
        let finalEndpoint = endpoint;
        const pathParamNames = new Set();
        
        if (currentEndpointInfo) {
            const pathParams = currentEndpointInfo.parameters.filter(p => p.in === 'path');
            pathParams.forEach(param => {
                pathParamNames.add(param.name);
                document.querySelectorAll('.param-row').forEach(row => {
                    const keyInput = row.querySelector('.param-key');
                    const valueInput = row.querySelector('.param-value');
                    if (keyInput && valueInput && keyInput.value.trim() === param.name) {
                        const value = valueInput.value.trim();
                        if (value) {
                            finalEndpoint = finalEndpoint.replace(`{${param.name}}`, encodeURIComponent(value));
                        }
                    }
                });
            });
        }
        
        // Build query parameters (excluding path params)
        const params = [];
        document.querySelectorAll('.param-row').forEach(row => {
            const keyInput = row.querySelector('.param-key');
            const valueInput = row.querySelector('.param-value');
            if (!keyInput || !valueInput) return;
            
            const key = keyInput.value.trim();
            const value = valueInput.value.trim();
            
            // Skip path parameters (they're already in the URL)
            if (pathParamNames.has(key)) {
                return;
            }
            
            if (key && value) {
                params.push(`${encodeURIComponent(key)}=${encodeURIComponent(value)}`);
            }
        });
        const queryString = params.length > 0 ? '?' + params.join('&') : '';
        
        const url = `${API_BASE_URL}${finalEndpoint}${queryString}`;
        
        // Generate curl command
            let cmd = `curl`;
            
            if (method !== 'GET') {
                cmd += ` -X ${method}`;
            }
            
            if (auth) {
                cmd += ` -H "Authorization: ${auth}"`;
            }
            
            if (body && (method === 'POST' || method === 'PUT' || method === 'PATCH')) {
                try {
                    // Validate JSON and format it
                    const bodyObj = JSON.parse(body);
                    const bodyStr = JSON.stringify(bodyObj);
                    // Escape quotes for shell
                    const escapedBody = bodyStr.replace(/"/g, '\\"');
                    cmd += ` -H "Content-Type: application/json" -d "${escapedBody}"`;
                } catch (e) {
                    // Invalid JSON, skip body
                }
            }
            
            cmd += ` "${url}"`;
            
            return cmd;
    } catch (error) {
        console.error('Error generating command:', error);
        return null;
    }
}

// Update the displayed request command
function updateRequestCommand() {
    const commandContent = document.getElementById('requestCommandContent');
    if (!commandContent) return; // Element doesn't exist yet
    
    const command = generateRequestCommand();
    
    if (command) {
        // Apply syntax highlighting to the command
        commandContent.innerHTML = `<pre class="json-viewer" style="margin: 0; white-space: pre-wrap; word-break: break-all;">${syntaxHighlightCommand(command)}</pre>`;
    } else {
        commandContent.innerHTML = '<div class="empty-state" style="color: var(--text-muted); font-size: 13px;">Select an endpoint to see the command</div>';
    }
}

// Syntax highlighting for shell commands (curl)
function syntaxHighlightCommand(cmd) {
    // Escape HTML first
    cmd = cmd.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    
    // Highlight command name
    cmd = cmd.replace(/^curl\b/, '<span style="color: #00ba7c; font-weight: 600;">curl</span>');
    
    // Highlight flags
    cmd = cmd.replace(/\s(-[XHdsFp]|--[a-z-]+)\b/g, ' <span style="color: #1d9bf0;">$1</span>');
    
    // Highlight HTTP methods
    cmd = cmd.replace(/\b(GET|POST|PUT|DELETE|PATCH)\b/g, '<span style="color: #ffd400;">$1</span>');
    
    // Highlight URLs (between quotes)
    cmd = cmd.replace(/("http[^"]+")/g, '<span style="color: #7856ff;">$1</span>');
    
    // Highlight JSON strings in -d flags
    cmd = cmd.replace(/(-d\s+['"])([^'"]+)(['"])/g, '$1<span style="color: #f4212e;">$2</span>$3');
    
    return cmd;
}

// Copy request command to clipboard
function copyRequestCommand() {
    const command = generateRequestCommand();
    if (command) {
        navigator.clipboard.writeText(command).then(() => {
            showToast('Command copied to clipboard', 'success', 3000);
        });
    }
}

// Set up event listeners for form fields to update command
function setupRequestCommandListeners() {
    // Listen to endpoint changes
    const endpointSelect = document.getElementById('requestEndpoint');
    if (endpointSelect) {
        endpointSelect.addEventListener('change', updateRequestCommand);
    }
    
    // Listen to auth changes
    const authInput = document.getElementById('requestAuth');
    if (authInput) {
        authInput.addEventListener('input', updateRequestCommand);
        authInput.addEventListener('change', updateRequestCommand);
    }
    
    // Listen to body changes
    const bodyInput = document.getElementById('requestBody');
    if (bodyInput) {
        bodyInput.addEventListener('input', updateRequestCommand);
        bodyInput.addEventListener('change', updateRequestCommand);
    }
    
    // Listen to query param changes (use MutationObserver for dynamic param rows)
    const paramsContainer = document.getElementById('queryParamsContainer');
    if (paramsContainer) {
        const observer = new MutationObserver(() => {
            updateRequestCommand();
        });
        observer.observe(paramsContainer, { childList: true, subtree: true });
        
        // Also listen to input events on param fields
        paramsContainer.addEventListener('input', (e) => {
            if (e.target.classList.contains('param-key') || e.target.classList.contains('param-value')) {
                updateRequestCommand();
            }
        });
    }
    
    // Listen to command type dropdown
    const commandTypeSelect = document.getElementById('requestCommandType');
    if (commandTypeSelect) {
        commandTypeSelect.addEventListener('change', updateRequestCommand);
    }
}

function syntaxHighlight(json) {
    json = json.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    return json.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+\.?\d*)/g, (match) => {
        let cls = 'json-number';
        if (/^"/.test(match)) {
            if (/:$/.test(match)) {
                cls = 'json-key';
            } else {
                cls = 'json-string';
            }
        } else if (/true|false/.test(match)) {
            cls = 'json-boolean';
        } else if (/null/.test(match)) {
            cls = 'json-null';
        }
        return `<span class="${cls}">${match}</span>`;
    });
}

// Toast Notification System
function showToast(message, type = 'info', duration = 5000) {
    const container = document.getElementById('toastContainer');
    if (!container) return;
    
    // Ensure container is positioned correctly (bottom center of viewport)
    container.style.position = 'fixed';
    container.style.top = 'auto';
    container.style.right = 'auto';
    container.style.left = '50%';
    container.style.bottom = '20px';
    container.style.transform = 'translateX(-50%)';
    container.style.zIndex = '10000';
    container.style.display = 'flex';
    container.style.flexDirection = 'column-reverse';
    container.style.gap = '12px';
    container.style.pointerEvents = 'none';
    container.style.alignItems = 'center';
    
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    
    // Apply inline styles to ensure they're applied (matching site's dark theme)
    toast.style.background = '#16181c'; // bg-secondary from theme
    toast.style.border = '1px solid #2f3336'; // border color from theme
    toast.style.borderRadius = '12px';
    toast.style.padding = '16px 20px';
    toast.style.minWidth = '320px';
    toast.style.maxWidth = '420px';
    toast.style.boxShadow = '0 8px 24px rgba(0, 0, 0, 0.4)';
    toast.style.display = 'flex';
    toast.style.alignItems = 'flex-start';
    toast.style.gap = '14px';
    toast.style.position = 'relative';
    toast.style.margin = '0';
    
    // Add colored left border based on type
    const borderColors = {
        success: '#00ba7c',
        error: '#f4212e',
        warning: '#ffd400',
        info: '#1d9bf0'
    };
    toast.style.borderLeft = `4px solid ${borderColors[type] || borderColors.info}`;
    
    const icons = {
        success: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></svg>',
        error: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></svg>',
        warning: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></svg>',
        info: '<svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="#ffffff" stroke-width="2"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></svg>'
    };
    
    const titles = {
        success: 'Success',
        error: 'Error',
        warning: 'Warning',
        info: 'Info'
    };
    
    toast.innerHTML = `
        <div class="toast-icon" style="flex-shrink: 0; margin-top: 2px;">${icons[type] || icons.info}</div>
        <div class="toast-content" style="flex: 1;">
            <div class="toast-title" style="font-weight: 600; font-size: 14px; color: #ffffff; margin-bottom: 4px;">${titles[type] || 'Info'}</div>
            <div class="toast-message" style="font-size: 13px; color: #e7e9ea; line-height: 1.5;">${message}</div>
        </div>
        <button class="toast-close" onclick="removeToast(this.parentElement)" style="background: rgba(255, 255, 255, 0.1); border: none; border-radius: 6px; color: #71767a; cursor: pointer; padding: 4px; width: 24px; height: 24px; display: flex; align-items: center; justify-content: center; flex-shrink: 0; transition: all 0.2s; opacity: 0.7;">
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="#71767a" stroke-width="2">
                <line x1="18" y1="6" x2="6" y2="18"></line>
                <line x1="6" y1="6" x2="18" y2="18"></line>
            </svg>
        </button>
    `;
    
    container.appendChild(toast);
    
    // Auto-remove after duration
    if (duration > 0) {
        setTimeout(() => {
            removeToast(toast);
        }, duration);
    }
    
    return toast;
}

function removeToast(toast) {
    if (!toast) return;
    toast.classList.add('closing');
    setTimeout(() => {
        if (toast.parentElement) {
            toast.parentElement.removeChild(toast);
        }
    }, 300);
}

// Confirmation Modal System
let currentConfirmResolve = null;

function showConfirm(title, message) {
    return new Promise((resolve) => {
        const modal = document.getElementById('confirmModal');
        const titleEl = document.getElementById('confirmModalTitle');
        const messageEl = document.getElementById('confirmModalMessage');
        const confirmBtn = document.getElementById('confirmModalConfirm');
        const cancelBtn = document.getElementById('confirmModalCancel');
        
        if (!modal || !titleEl || !messageEl || !confirmBtn || !cancelBtn) {
            // Fallback to native confirm if modal doesn't exist
            console.warn('Confirm modal elements not found, using native confirm');
            resolve(confirm(message));
            return;
        }
        
        // Cancel any pending confirmation
        if (currentConfirmResolve) {
            currentConfirmResolve(false);
        }
        
        // Validate inputs - don't show empty modal
        if (!title || !title.trim()) {
            title = 'Confirm';
        }
        if (!message || !message.trim()) {
            // Don't show modal if message is empty
            resolve(false);
            return;
        }
        
        currentConfirmResolve = resolve;
        titleEl.textContent = title;
        messageEl.textContent = message;
        
        // Ensure elements are visible
        titleEl.style.display = '';
        messageEl.style.display = '';
        
        // Store current scroll position BEFORE showing modal
        const scrollY = window.scrollY || window.pageYOffset || document.documentElement.scrollTop;
        
        // Explicitly set flexbox properties to ensure centering works
        // Use inline styles with !important to override any conflicting styles
        modal.style.cssText = `
            position: fixed !important;
            top: 0 !important;
            left: 0 !important;
            right: 0 !important;
            bottom: 0 !important;
            width: 100vw !important;
            height: 100vh !important;
            background: rgba(0, 0, 0, 0.7) !important;
            display: flex !important;
            align-items: center !important;
            justify-content: center !important;
            z-index: 10001 !important;
            padding: 20px !important;
            box-sizing: border-box !important;
            margin: 0 !important;
            overflow: auto !important;
        `;
        
        // Also add the show class for CSS animations
        modal.classList.add('show');
        
        // Immediately prevent scroll
        requestAnimationFrame(() => {
            window.scrollTo(0, scrollY);
            document.documentElement.scrollTop = scrollY;
            document.body.scrollTop = scrollY;
        });
        
        // Focus the confirm button for keyboard navigation (prevent scroll)
        setTimeout(() => {
            // Ensure scroll position is maintained
            window.scrollTo(0, scrollY);
            document.documentElement.scrollTop = scrollY;
            document.body.scrollTop = scrollY;
            
            if (confirmBtn && typeof confirmBtn.focus === 'function') {
                try {
                    confirmBtn.focus({ preventScroll: true });
                } catch (e) {
                    // Fallback for browsers that don't support preventScroll
                    confirmBtn.focus();
                    window.scrollTo(0, scrollY);
                }
            }
        }, 50);
    });
}

// Set up modal button handlers once (on page load)
document.addEventListener('DOMContentLoaded', () => {
    const modal = document.getElementById('confirmModal');
    const confirmBtn = document.getElementById('confirmModalConfirm');
    const cancelBtn = document.getElementById('confirmModalCancel');
    
    if (modal && confirmBtn && cancelBtn) {
        confirmBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            e.preventDefault();
            if (currentConfirmResolve) {
                modal.classList.remove('show');
                modal.style.display = 'none';
                const resolve = currentConfirmResolve;
                currentConfirmResolve = null;
                resolve(true);
            }
        });
        
        cancelBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            e.preventDefault();
            if (currentConfirmResolve) {
                modal.classList.remove('show');
                modal.style.display = 'none';
                const resolve = currentConfirmResolve;
                currentConfirmResolve = null;
                resolve(false);
            }
        });
        
        modal.addEventListener('click', (e) => {
            // Only close if clicking the overlay itself, not the modal content
            if (e.target === modal && currentConfirmResolve) {
                e.preventDefault();
                e.stopPropagation();
                modal.classList.remove('show');
                modal.style.display = 'none';
                const resolve = currentConfirmResolve;
                currentConfirmResolve = null;
                resolve(false);
            }
        });
        
        // Prevent clicks inside modal content from closing the modal (only add once)
        const modalContent = modal.querySelector('.modal-content');
        if (modalContent && !modalContent.dataset.listenerAdded) {
            modalContent.addEventListener('click', (e) => {
                e.stopPropagation();
            });
            modalContent.dataset.listenerAdded = 'true';
        }
        
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && modal.classList.contains('show') && currentConfirmResolve) {
                e.preventDefault();
                e.stopPropagation();
                modal.classList.remove('show');
                modal.style.display = 'none';
                const resolve = currentConfirmResolve;
                currentConfirmResolve = null;
                resolve(false);
            }
        });
        
        // Ensure modal is hidden on page load
        if (modal) {
            modal.classList.remove('show');
        }
    }
});

// Configuration
async function loadConfig() {
    try {
        // Load config data
        await loadConfigData();
    } catch (error) {
        const content = document.getElementById('configContent');
        if (content) {
            content.innerHTML = 
                `<div style="color: var(--danger);">Failed to load configuration: ${error.message}</div>`;
        }
    }
}

let currentConfig = null;
let configEditMode = false;

async function loadConfigData() {
    try {
        const response = await fetch(`${API_BASE_URL}/config`);
        const data = await response.json();
        
        // The API returns { config: {...}, note: "..." }, so extract the config object
        const config = data.config || data;
        // Deep clone to avoid mutating the original
        currentConfig = JSON.parse(JSON.stringify(config));
        
        const content = document.getElementById('configContent');
        if (!content) {
            console.error('configContent element not found');
            return;
        }
        
        if (!config || Object.keys(config).length === 0) {
            content.innerHTML = '<div class="empty-state">No configuration available</div>';
            currentConfig = {};
            return;
        }
        
        if (!configEditMode) {
            content.innerHTML = renderConfig(config);
        } else {
            renderConfigEditor(currentConfig);
        }
    } catch (error) {
        const content = document.getElementById('configContent');
        if (content) {
            content.innerHTML = 
                `<div style="color: var(--danger);">Failed to load configuration: ${error.message}</div>`;
        } else {
            console.error('Failed to load configuration and configContent element not found:', error);
        }
    }
}

function toggleConfigView() {
    configEditMode = !configEditMode;
    const content = document.getElementById('configContent');
    const editor = document.getElementById('configEditor');
    const toggleBtn = document.getElementById('toggleConfigViewBtn');
    const toggleText = document.getElementById('toggleConfigViewText');
    
    if (configEditMode) {
        content.style.display = 'none';
        editor.style.display = 'block';
        toggleText.textContent = 'View Mode';
        if (currentConfig) {
            renderConfigEditor(currentConfig);
        }
    } else {
        content.style.display = 'block';
        editor.style.display = 'none';
        toggleText.textContent = 'Edit Mode';
        if (currentConfig) {
            content.innerHTML = renderConfig(currentConfig);
        }
    }
}

function switchConfigTab(tab) {
    const formTab = document.getElementById('configTabForm');
    const jsonTab = document.getElementById('configTabJson');
    const formEditor = document.getElementById('configFormEditor');
    const jsonEditor = document.getElementById('configJsonEditor');
    
    if (tab === 'form') {
        formTab.classList.add('active');
        jsonTab.classList.remove('active');
        formEditor.style.display = 'block';
        jsonEditor.style.display = 'none';
        // Parse JSON and update form if JSON was edited
        try {
            const jsonText = document.getElementById('configJsonTextarea').value;
            const parsed = JSON.parse(jsonText);
            currentConfig = parsed;
            renderConfigEditor(parsed);
        } catch (e) {
            // Invalid JSON, keep current form state
        }
    } else {
        formTab.classList.remove('active');
        jsonTab.classList.add('active');
        formEditor.style.display = 'none';
        jsonEditor.style.display = 'block';
        // Update JSON editor with current form values
        updateJsonEditor();
    }
}

function renderConfigEditor(config) {
    const editor = document.getElementById('configFormEditor');
    let html = '<div class="config-form">';
    
    // Unified Rate Limiting Section
    html += renderUnifiedRateLimitingSection(config);
    
    // Persistence Section
    html += renderConfigSection('persistence', 'State Persistence', {
        enabled: { 
            type: 'checkbox', 
            label: 'Enabled', 
            description: 'Save playground data to disk for persistence across restarts (default: enabled)',
            value: config.persistence?.enabled !== false 
        },
        file_path: { 
            type: 'text', 
            label: 'File Path', 
            description: 'Path to state file (default: ~/.playground/state.json)',
            value: config.persistence?.file_path || '' 
        },
        auto_save: { 
            type: 'checkbox', 
            label: 'Auto-save', 
            description: 'Automatically save state changes periodically',
            value: config.persistence?.auto_save !== false 
        },
        save_interval: { 
            type: 'number', 
            label: 'Save Interval (seconds)', 
            description: 'How often to auto-save (default: 60 seconds)',
            value: config.persistence?.save_interval || 60 
        }
    }, config.persistence);
    
    // Authentication Section
    html += renderConfigSection('auth', 'Authentication', {
        disable_validation: { 
            type: 'checkbox', 
            label: 'Disable Authentication Validation', 
            description: 'When enabled, allows requests without Authorization headers (useful for testing). When disabled, enforces authentication like the real X API.',
            value: config.auth?.disable_validation || false 
        }
    }, config.auth);
    
    // Streaming Section
    html += renderConfigSection('streaming', 'Streaming', {
        default_delay_ms: { 
            type: 'number', 
            label: 'Default Delay (ms)', 
            description: 'Delay between streamed events (default: 100ms)',
            value: config.streaming?.default_delay_ms || 100 
        }
    }, config.streaming);
    
    // Error Simulation Section
    html += renderConfigSection('errors', 'Error Simulation', {
        enabled: { 
            type: 'checkbox', 
            label: 'Enabled', 
            description: 'Randomly return errors to test error handling',
            value: config.errors?.enabled || false 
        },
        error_rate: { 
            type: 'number', 
            label: 'Error Rate (0.0-1.0)', 
            step: '0.01', 
            min: '0', 
            max: '1', 
            description: 'Error probability (0.0 = never, 1.0 = always, 0.1 = 10%)',
            value: config.errors?.error_rate || 0 
        },
        error_type: { 
            type: 'select', 
            label: 'Error Type', 
            options: ['rate_limit', 'server_error', 'unauthorized'], 
            description: 'Error type determines status code automatically: rate_limit (429), server_error (500), unauthorized (401)',
            value: config.errors?.error_type || 'rate_limit' 
        }
    }, config.errors);
    
    html += '</div>';
    editor.innerHTML = html;
    
    // Update JSON editor
    document.getElementById('configJsonTextarea').value = JSON.stringify(config, null, 2);
    
    // Initialize rate limit overrides
    window.rateLimitOverrides = config?.rate_limit?.endpoint_overrides || {};
    
    // Load endpoints when rate limiting section is visible
    const rateLimitSection = document.querySelector('[data-section="rate_limit"]');
    if (rateLimitSection && (!window.rateLimitEndpoints || window.rateLimitEndpoints.length === 0)) {
        // Load in background
        loadEndpointsForRateLimits().catch(() => {});
    }
}

async function loadEndpointsForRateLimits() {
    try {
        const [rateLimitsResponse, endpointsResponse] = await Promise.all([
            fetch(`${API_BASE_URL}/rate-limits`),
            fetch(`${API_BASE_URL}/endpoints`).catch(() => null)
        ]);
        
        const rateLimitsData = await rateLimitsResponse.json();
        let allEndpoints = [];
        
        if (endpointsResponse && endpointsResponse.ok) {
            const endpointsData = await endpointsResponse.json();
            allEndpoints = endpointsData.endpoints || [];
        }
        
        // Store for use in endpoint editor
        window.rateLimitEndpoints = allEndpoints;
        window.rateLimitDefaults = rateLimitsData.endpoints || [];
        window.rateLimitOverrides = currentConfig?.rate_limit?.endpoint_overrides || {};
        
        // Create a map of default limits for quick lookup
        window.rateLimitDefaultsMap = {};
        (rateLimitsData.endpoints || []).forEach(ep => {
            const key = ep.method ? `${ep.method}:${ep.endpoint}` : ep.endpoint;
            window.rateLimitDefaultsMap[key] = { limit: ep.limit, window_sec: ep.window_sec, source: 'default' };
        });
        
        // Render endpoint overrides section
        refreshEndpointOverridesList();
        
        showToast('Endpoints loaded successfully', 'success');
    } catch (error) {
        console.error('Failed to load endpoints for rate limits:', error);
        showToast('Failed to load endpoints: ' + error.message, 'error');
    }
}

async function showAllEndpointsEditor() {
    // Load endpoints if not already loaded
    if (!window.rateLimitEndpoints || window.rateLimitEndpoints.length === 0) {
        await loadEndpointsForRateLimits();
    }
    
    if (!window.rateLimitEndpoints || window.rateLimitEndpoints.length === 0) {
        showToast('No endpoints available. Make sure OpenAPI spec is loaded.', 'error');
        return;
    }
    
    // Create modal for endpoint editor
    const overlay = document.createElement('div');
    overlay.style.cssText = 'position: fixed; top: 0; left: 0; width: 100vw; height: 100vh; background: rgba(0,0,0,0.7); z-index: 10000; display: flex; align-items: center; justify-content: center;';
    
    const modal = document.createElement('div');
    modal.style.cssText = 'background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 12px; padding: 24px; width: 90vw; max-width: 1000px; max-height: 90vh; display: flex; flex-direction: column;';
    
    // Build endpoint list with current limits
    const endpointsWithLimits = window.rateLimitEndpoints.map(ep => {
        const key = `${ep.method}:${ep.path}`;
        const pathOnlyKey = ep.path;
        const override = window.rateLimitOverrides[key] || window.rateLimitOverrides[pathOnlyKey];
        const defaultLimit = window.rateLimitDefaultsMap[key] || window.rateLimitDefaultsMap[pathOnlyKey];
        
        return {
            method: ep.method,
            path: ep.path,
            key: key,
            pathOnlyKey: pathOnlyKey,
            override: override,
            defaultLimit: defaultLimit,
            currentLimit: override || (defaultLimit ? { limit: defaultLimit.limit, window_sec: defaultLimit.window_sec } : null),
            source: override ? 'override' : (defaultLimit ? 'default' : 'none')
        };
    });
    
    let searchFilter = '';
    
    const renderEndpointsList = () => {
        const filtered = endpointsWithLimits.filter(ep => {
            if (!searchFilter) return true;
            const search = searchFilter.toLowerCase();
            return ep.path.toLowerCase().includes(search) || 
                   ep.method.toLowerCase().includes(search) ||
                   (ep.summary && ep.summary.toLowerCase().includes(search));
        });
        
        return filtered.map(ep => {
            const methodColor = ep.method === 'GET' ? '#1d9bf0' : ep.method === 'POST' ? '#00ba7c' : ep.method === 'DELETE' ? '#f4212e' : '#71767a';
            const sourceBadge = ep.source === 'override' ? '<span style="background: var(--primary); color: white; padding: 2px 6px; border-radius: 3px; font-size: 10px; margin-left: 8px;">OVERRIDE</span>' :
                              ep.source === 'default' ? '<span style="background: var(--bg-tertiary); color: var(--text-secondary); padding: 2px 6px; border-radius: 3px; font-size: 10px; margin-left: 8px;">DEFAULT</span>' :
                              '<span style="background: var(--bg-tertiary); color: var(--text-muted); padding: 2px 6px; border-radius: 3px; font-size: 10px; margin-left: 8px;">NONE</span>';
            
            const limit = ep.currentLimit ? `${ep.currentLimit.limit}/${ep.currentLimit.window_sec}s` : 'default';
            
            return `
                <div style="display: flex; gap: 12px; align-items: center; padding: 10px; background: var(--bg-primary); border-radius: 6px; border: 1px solid var(--border);">
                    <div style="flex: 1; min-width: 0;">
                        <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 4px;">
                            <span style="color: ${methodColor}; font-weight: 600; min-width: 60px; font-size: 12px;">${ep.method}</span>
                            <span style="font-family: monospace; font-size: 12px; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${ep.path}</span>
                            ${sourceBadge}
                        </div>
                        <div style="font-size: 11px; color: var(--text-secondary);">
                            Limit: <strong>${limit}</strong>
                        </div>
                    </div>
                    <button onclick="editEndpointFromList('${ep.key.replace(/'/g, "\\'")}', '${ep.pathOnlyKey.replace(/'/g, "\\'")}', '${ep.method}', '${ep.path.replace(/'/g, "\\'")}')" style="padding: 6px 12px; background: var(--primary); color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 12px;">
                        ${ep.source === 'override' ? 'Edit' : 'Set Override'}
                    </button>
                </div>
            `;
        }).join('');
    };
    
    modal.innerHTML = `
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px;">
            <h3 style="margin: 0; color: var(--text-primary);">All Endpoints Rate Limits</h3>
            <button id="closeEndpointsModal" style="padding: 6px 12px; background: var(--bg-tertiary); color: var(--text-primary); border: 1px solid var(--border); border-radius: 6px; cursor: pointer; font-size: 12px;">Close</button>
        </div>
        <div style="margin-bottom: 16px;">
            <input type="text" id="endpointSearchInput" placeholder="Search endpoints by path, method, or summary..." style="width: 100%; padding: 8px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-primary); color: var(--text-primary); font-size: 13px;" oninput="updateEndpointsList()">
        </div>
        <div style="flex: 1; overflow-y: auto; border: 1px solid var(--border); border-radius: 6px; padding: 12px; background: var(--bg-primary);">
            <div id="endpointsListContainer" style="display: flex; flex-direction: column; gap: 8px;">
                ${renderEndpointsList()}
            </div>
        </div>
        <div style="margin-top: 12px; font-size: 12px; color: var(--text-muted);">
            Showing ${endpointsWithLimits.length} endpoint${endpointsWithLimits.length !== 1 ? 's' : ''}. 
            <span style="background: var(--primary); color: white; padding: 2px 6px; border-radius: 3px; font-size: 10px;">OVERRIDE</span> = User configured, 
            <span style="background: var(--bg-tertiary); color: var(--text-secondary); padding: 2px 6px; border-radius: 3px; font-size: 10px;">DEFAULT</span> = Hardcoded default, 
            <span style="background: var(--bg-tertiary); color: var(--text-muted); padding: 2px 6px; border-radius: 3px; font-size: 10px;">NONE</span> = Uses configurable default
        </div>
    `;
    
    overlay.appendChild(modal);
    document.body.appendChild(overlay);
    
    // Store references for search
    window.endpointsModalOverlay = overlay;
    window.endpointsModalSearchFilter = searchFilter;
    window.endpointsWithLimits = endpointsWithLimits;
    
    overlay.querySelector('#closeEndpointsModal').addEventListener('click', () => {
        document.body.removeChild(overlay);
        delete window.endpointsModalOverlay;
    });
    
    overlay.addEventListener('click', (e) => {
        if (e.target === overlay) {
            document.body.removeChild(overlay);
            delete window.endpointsModalOverlay;
        }
    });
    
    // Make search function available
    window.updateEndpointsList = () => {
        const input = overlay.querySelector('#endpointSearchInput');
        window.endpointsModalSearchFilter = input.value;
        const container = overlay.querySelector('#endpointsListContainer');
        container.innerHTML = renderEndpointsList();
    };
    
    // Make edit function available
    window.editEndpointFromList = async (key, pathOnlyKey, method, path) => {
        // Check if there's an existing override
        const existingOverride = window.rateLimitOverrides[key] || window.rateLimitOverrides[pathOnlyKey];
        const defaultLimit = window.rateLimitDefaultsMap[key] || window.rateLimitDefaultsMap[pathOnlyKey];
        
        const currentLimit = existingOverride ? existingOverride.limit : (defaultLimit ? defaultLimit.limit : 15);
        const currentWindow = existingOverride ? existingOverride.window_sec : (defaultLimit ? defaultLimit.window_sec : 900);
        
        const result = await showEndpointOverrideDialog(
            `Edit Rate Limit: ${method} ${path}`,
            key,
            currentLimit.toString(),
            currentWindow.toString()
        );
        
        if (!result || !result.key) return;
        
        // Add/update override
        if (!currentConfig.rate_limit) {
            currentConfig.rate_limit = {};
        }
        if (!currentConfig.rate_limit.endpoint_overrides) {
            currentConfig.rate_limit.endpoint_overrides = {};
        }
        
        currentConfig.rate_limit.endpoint_overrides[result.key.trim()] = {
            limit: parseInt(result.limit) || 15,
            window_sec: parseInt(result.windowSec) || 900
        };
        
        window.rateLimitOverrides = currentConfig.rate_limit.endpoint_overrides;
        updateJsonEditor();
        
        // Refresh the modal
        showAllEndpointsEditor();
    };
}


function renderUnifiedRateLimitingSection(config) {
    const hasRateLimit = config.rate_limit !== undefined && config.rate_limit !== null;
    const hasOverrides = config.rate_limit?.endpoint_overrides && Object.keys(config.rate_limit.endpoint_overrides).length > 0;
    const overrideCount = hasOverrides ? Object.keys(config.rate_limit.endpoint_overrides).length : 0;
    
    return `
        <div class="config-form-section" data-section="rate_limit" style="border: 2px solid var(--border); border-radius: 8px; padding: 20px; background: var(--bg-secondary);">
            <div style="margin-bottom: 20px;">
                <h3 style="margin: 0 0 8px 0; color: var(--text-primary); font-size: 18px;">Rate Limiting</h3>
                <p style="font-size: 13px; color: var(--text-secondary); line-height: 1.5; margin: 0;">
                    Configure rate limiting simulation. Endpoint-specific overrides take priority over hardcoded defaults, which take priority over the configurable default.
                </p>
            </div>
            
            <!-- Enable Toggle -->
            <div style="margin-bottom: 24px; padding: 16px; background: var(--bg-primary); border-radius: 6px; border: 1px solid var(--border);">
                <label class="toggle-container">
                    <span class="toggle-switch">
                        <input type="checkbox" id="rate_limit_enabled" ${config.rate_limit?.enabled !== false ? 'checked' : ''} onchange="updateConfigValue('rate_limit', 'enabled', this.checked)">
                        <span class="toggle-slider"></span>
                    </span>
                    <div class="toggle-label">
                        <div style="font-weight: 600; color: var(--text-primary); margin-bottom: 4px;">Enable Rate Limiting</div>
                        <div style="font-size: 12px; color: var(--text-secondary);">When enabled, the playground enforces rate limits on API requests</div>
                    </div>
                </label>
            </div>
            
            <!-- Default Settings -->
            <div style="margin-bottom: 24px; padding: 16px; background: var(--bg-primary); border-radius: 6px; border: 1px solid var(--border);">
                <div style="font-weight: 600; color: var(--text-primary); margin-bottom: 12px; font-size: 14px;">Default Rate Limit</div>
                <div style="font-size: 12px; color: var(--text-secondary); margin-bottom: 16px; line-height: 1.5;">
                    Used for endpoints without hardcoded defaults or user overrides. Most endpoints from the X API use this default unless they have a specific limit configured.
                </div>
                <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 16px;">
                    <div>
                        <label for="rate_limit_limit" style="display: block; font-size: 12px; color: var(--text-secondary); margin-bottom: 6px;">
                            Limit (requests per window)
                            <span class="config-field-tooltip">
                                <svg class="config-field-tooltip-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <circle cx="12" cy="12" r="10"></circle>
                                    <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"></path>
                                    <line x1="12" y1="17" x2="12.01" y2="17"></line>
                                </svg>
                                <span class="config-field-tooltip-text">Number of requests allowed per time window</span>
                            </span>
                        </label>
                        <input type="number" id="rate_limit_limit" value="${config.rate_limit?.limit || 15}" placeholder="15" onchange="updateConfigValue('rate_limit', 'limit', parseInt(this.value) || 15)" style="width: 100%; padding: 8px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-secondary); color: var(--text-primary);">
                    </div>
                    <div>
                        <label for="rate_limit_window_sec" style="display: block; font-size: 12px; color: var(--text-secondary); margin-bottom: 6px;">
                            Window (seconds)
                            <span class="config-field-tooltip">
                                <svg class="config-field-tooltip-icon" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                                    <circle cx="12" cy="12" r="10"></circle>
                                    <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"></path>
                                    <line x1="12" y1="17" x2="12.01" y2="17"></line>
                                </svg>
                                <span class="config-field-tooltip-text">Time window in seconds (default: 900 = 15 minutes)</span>
                            </span>
                        </label>
                        <input type="number" id="rate_limit_window_sec" value="${config.rate_limit?.window_sec || 900}" placeholder="900" onchange="updateConfigValue('rate_limit', 'window_sec', parseInt(this.value) || 900)" style="width: 100%; padding: 8px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-secondary); color: var(--text-primary);">
                    </div>
                </div>
            </div>
            
            <!-- Endpoint Management -->
            <div style="padding: 16px; background: var(--bg-primary); border-radius: 6px; border: 1px solid var(--border);">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px;">
                    <div>
                        <div style="font-weight: 600; color: var(--text-primary); margin-bottom: 4px; font-size: 14px;">Endpoint-Specific Limits</div>
                        <div style="font-size: 12px; color: var(--text-secondary);">
                            ${overrideCount > 0 ? `${overrideCount} user override${overrideCount !== 1 ? 's' : ''} configured` : 'No overrides configured'}
                        </div>
                    </div>
                    <button onclick="showAllEndpointsEditor()" style="padding: 8px 16px; background: var(--primary); color: white; border: none; border-radius: 6px; cursor: pointer; font-size: 13px; font-weight: 500;">
                        Manage Endpoints
                    </button>
                </div>
                
                <div style="font-size: 12px; color: var(--text-secondary); margin-bottom: 12px; line-height: 1.5; padding: 12px; background: var(--bg-secondary); border-radius: 4px;">
                    <strong style="color: var(--text-primary);">Priority Order:</strong><br>
                    1. <strong>User Overrides</strong> (configured here) - Highest priority<br>
                    2. <strong>Hardcoded Defaults</strong> (~50 endpoints matching X API) - Medium priority<br>
                    3. <strong>Configurable Default</strong> (above) - Lowest priority
                </div>
                
                ${hasOverrides ? `
                    <div id="endpointOverridesList" style="margin-top: 12px;">
                        ${renderEndpointOverridesList()}
                    </div>
                ` : `
                    <div style="text-align: center; padding: 24px; color: var(--text-muted); font-size: 13px;">
                        Click "Manage Endpoints" to view all endpoints and configure overrides
                    </div>
                `}
            </div>
        </div>
    `;
}

function renderEndpointOverridesList() {
    if (!window.rateLimitOverrides || Object.keys(window.rateLimitOverrides).length === 0) {
        return '<div style="color: var(--text-muted); font-style: italic; padding: 16px; text-align: center;">No endpoint overrides configured. Click "Add Override" to create one.</div>';
    }
    
    let html = '<div style="display: flex; flex-direction: column; gap: 8px;">';
    for (const [key, override] of Object.entries(window.rateLimitOverrides)) {
        html += `
            <div style="display: flex; gap: 8px; align-items: center; padding: 12px; background: var(--bg-secondary); border-radius: 6px; border: 1px solid var(--border);">
                <div style="flex: 1; min-width: 0;">
                    <div style="font-family: monospace; font-size: 12px; color: var(--text-primary); margin-bottom: 4px; word-break: break-all;">${key}</div>
                    <div style="display: flex; gap: 16px; font-size: 12px; color: var(--text-secondary);">
                        <span>Limit: <strong>${override.limit}</strong></span>
                        <span>Window: <strong>${override.window_sec}s</strong></span>
                    </div>
                </div>
                <button onclick="editEndpointOverride('${key.replace(/'/g, "\\'")}')" style="padding: 6px 12px; background: var(--bg-tertiary); color: var(--text-primary); border: 1px solid var(--border); border-radius: 4px; cursor: pointer; font-size: 12px;">
                    Edit
                </button>
                <button onclick="removeEndpointOverride('${key.replace(/'/g, "\\'")}')" style="padding: 6px 12px; background: var(--danger); color: white; border: none; border-radius: 4px; cursor: pointer; font-size: 12px;">
                    Remove
                </button>
            </div>
        `;
    }
    html += '</div>';
    return html;
}

async function addEndpointOverride() {
    // Show a simple form for adding override
    const key = await showEndpointOverrideDialog('Add Endpoint Override', '', '', '');
    if (!key || !key.key) return;
    
    if (!currentConfig.rate_limit) {
        currentConfig.rate_limit = {};
    }
    if (!currentConfig.rate_limit.endpoint_overrides) {
        currentConfig.rate_limit.endpoint_overrides = {};
    }
    
    currentConfig.rate_limit.endpoint_overrides[key.key.trim()] = {
        limit: parseInt(key.limit) || 15,
        window_sec: parseInt(key.windowSec) || 900
    };
    
    window.rateLimitOverrides = currentConfig.rate_limit.endpoint_overrides;
    updateJsonEditor();
    
    // Re-render the section
    refreshEndpointOverridesList();
}

async function editEndpointOverride(key) {
    const override = window.rateLimitOverrides[key];
    if (!override) return;
    
    const result = await showEndpointOverrideDialog('Edit Endpoint Override', key, override.limit.toString(), override.window_sec.toString());
    if (!result || !result.key) return;
    
    // If key changed, remove old and add new
    if (result.key !== key) {
        delete currentConfig.rate_limit.endpoint_overrides[key];
    }
    
    currentConfig.rate_limit.endpoint_overrides[result.key.trim()] = {
        limit: parseInt(result.limit) || 15,
        window_sec: parseInt(result.windowSec) || 900
    };
    
    window.rateLimitOverrides = currentConfig.rate_limit.endpoint_overrides;
    updateJsonEditor();
    
    refreshEndpointOverridesList();
}

async function removeEndpointOverride(key) {
    const confirmed = await showConfirm('Remove Override', `Remove rate limit override for "${key}"?`);
    if (!confirmed) return;
    
    delete currentConfig.rate_limit.endpoint_overrides[key];
    
    if (Object.keys(currentConfig.rate_limit.endpoint_overrides).length === 0) {
        delete currentConfig.rate_limit.endpoint_overrides;
    }
    
    window.rateLimitOverrides = currentConfig.rate_limit.endpoint_overrides || {};
    updateJsonEditor();
    
    refreshEndpointOverridesList();
}

function refreshEndpointOverridesList() {
    // Refresh in the unified rate limiting section
    const section = document.querySelector('[data-section="rate_limit"]');
    if (section) {
        const listDiv = section.querySelector('#endpointOverridesList');
        if (listDiv) {
            listDiv.innerHTML = renderEndpointOverridesList();
        } else {
            // Re-render the entire section if structure changed
            if (currentConfig) {
                renderConfigEditor(currentConfig);
            }
        }
    }
}

function showEndpointOverrideDialog(title, currentKey, currentLimit, currentWindowSec) {
    return new Promise((resolve) => {
        // Create modal overlay
        const overlay = document.createElement('div');
        overlay.style.cssText = 'position: fixed; top: 0; left: 0; width: 100vw; height: 100vh; background: rgba(0,0,0,0.7); z-index: 10000; display: flex; align-items: center; justify-content: center;';
        
        const modal = document.createElement('div');
        modal.style.cssText = 'background: var(--bg-secondary); border: 1px solid var(--border); border-radius: 12px; padding: 24px; min-width: 400px; max-width: 500px;';
        
        modal.innerHTML = `
            <h3 style="margin: 0 0 16px 0; color: var(--text-primary);">${title}</h3>
            <div style="display: flex; flex-direction: column; gap: 12px;">
                <div>
                    <label style="display: block; margin-bottom: 4px; color: var(--text-secondary); font-size: 13px;">Endpoint Key</label>
                    <input type="text" id="overrideKey" value="${currentKey}" placeholder='e.g., "GET:/2/users/me" or "/2/users"' style="width: 100%; padding: 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-primary); color: var(--text-primary); font-family: monospace; font-size: 12px;">
                    <div style="font-size: 11px; color: var(--text-muted); margin-top: 4px;">Format: "METHOD:ENDPOINT" or "ENDPOINT"</div>
                </div>
                <div>
                    <label style="display: block; margin-bottom: 4px; color: var(--text-secondary); font-size: 13px;">Limit (requests per window)</label>
                    <input type="number" id="overrideLimit" value="${currentLimit}" placeholder="15" style="width: 100%; padding: 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-primary); color: var(--text-primary);">
                </div>
                <div>
                    <label style="display: block; margin-bottom: 4px; color: var(--text-secondary); font-size: 13px;">Window (seconds)</label>
                    <input type="number" id="overrideWindowSec" value="${currentWindowSec}" placeholder="900" style="width: 100%; padding: 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-primary); color: var(--text-primary);">
                </div>
            </div>
            <div style="display: flex; gap: 8px; justify-content: flex-end; margin-top: 20px;">
                <button id="cancelOverride" style="padding: 8px 16px; background: var(--bg-tertiary); color: var(--text-primary); border: 1px solid var(--border); border-radius: 6px; cursor: pointer;">Cancel</button>
                <button id="saveOverride" style="padding: 8px 16px; background: var(--primary); color: white; border: none; border-radius: 6px; cursor: pointer;">Save</button>
            </div>
        `;
        
        overlay.appendChild(modal);
        document.body.appendChild(overlay);
        
        const keyInput = modal.querySelector('#overrideKey');
        keyInput.focus();
        if (!currentKey) {
            keyInput.select();
        }
        
        const cleanup = () => {
            document.body.removeChild(overlay);
        };
        
        modal.querySelector('#cancelOverride').addEventListener('click', () => {
            cleanup();
            resolve(null);
        });
        
        modal.querySelector('#saveOverride').addEventListener('click', () => {
            const key = keyInput.value.trim();
            const limit = modal.querySelector('#overrideLimit').value;
            const windowSec = modal.querySelector('#overrideWindowSec').value;
            
            if (!key) {
                showToast('Endpoint key is required', 'error');
                return;
            }
            
            cleanup();
            resolve({ key, limit, windowSec });
        });
        
        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) {
                cleanup();
                resolve(null);
            }
        });
        
        modal.querySelectorAll('input').forEach(input => {
            input.addEventListener('keydown', (e) => {
                if (e.key === 'Enter') {
                    modal.querySelector('#saveOverride').click();
                } else if (e.key === 'Escape') {
                    modal.querySelector('#cancelOverride').click();
                }
            });
        });
    });
}

// Settings that require server restart to take effect
// Note: Rate limiting and persistence now reload dynamically, so no restart needed
const RESTART_REQUIRED_SETTINGS = {};

function requiresRestart(sectionKey, fieldKey) {
    return RESTART_REQUIRED_SETTINGS[sectionKey] && RESTART_REQUIRED_SETTINGS[sectionKey].includes(fieldKey);
}

function renderConfigSection(sectionKey, sectionName, fields, existingSection) {
    // Check if any field in this section requires restart
    const hasRestartRequired = Object.keys(fields).some(fieldKey => requiresRestart(sectionKey, fieldKey));
    // Check if ALL fields require restart (if so, skip field-level badges)
    const allFieldsRequireRestart = hasRestartRequired && 
        Object.keys(fields).every(fieldKey => requiresRestart(sectionKey, fieldKey));
    const restartBadge = hasRestartRequired ? '<span style="background: #f59e0b; color: white; padding: 2px 8px; border-radius: 4px; font-size: 11px; font-weight: 600; margin-left: 8px;">⚠️ Requires Restart</span>' : '';
    
    let html = `
        <div class="config-form-section" data-section="${sectionKey}">
            <div class="config-form-section-header">
                <strong>${sectionName}</strong>${restartBadge}
            </div>
            <div class="config-form-section-content">
    `;
    
    for (const [fieldKey, fieldConfig] of Object.entries(fields)) {
        html += renderConfigField(sectionKey, fieldKey, fieldConfig, existingSection?.[fieldKey], allFieldsRequireRestart);
    }
    
    html += `
            </div>
        </div>
    `;
    return html;
}

function renderConfigField(sectionKey, fieldKey, fieldConfig, value, skipFieldLevelBadge = false) {
    const id = `${sectionKey}_${fieldKey}`;
    const fieldValue = value !== undefined ? value : (fieldConfig.value !== undefined ? fieldConfig.value : '');
    
    // Define placeholders for different fields
    const placeholders = {
        'persistence_file_path': '~/.playground/state.json',
        'rate_limit_limit': '15',
        'rate_limit_window_sec': '900',
        'streaming_default_delay_ms': '100',
        'errors_error_rate': '0.0',
        'persistence_save_interval': '60'
    };
    
    const placeholderKey = `${sectionKey}_${fieldKey}`;
    const placeholder = placeholders[placeholderKey] || '';
    
    let input = '';
    switch (fieldConfig.type) {
        case 'checkbox':
            input = `
                <label class="toggle-container">
                    <span class="toggle-switch">
                        <input type="checkbox" id="${id}" ${fieldValue ? 'checked' : ''} onchange="updateConfigValue('${sectionKey}', '${fieldKey}', this.checked)">
                        <span class="toggle-slider"></span>
                    </span>
                </label>
            `;
            break;
        case 'number':
            const attrs = [];
            if (fieldConfig.step) attrs.push(`step="${fieldConfig.step}"`);
            if (fieldConfig.min !== undefined) attrs.push(`min="${fieldConfig.min}"`);
            if (fieldConfig.max !== undefined) attrs.push(`max="${fieldConfig.max}"`);
            if (placeholder) attrs.push(`placeholder="${placeholder}"`);
            input = `<input type="number" id="${id}" value="${fieldValue}" ${attrs.join(' ')} onchange="updateConfigValue('${sectionKey}', '${fieldKey}', parseFloat(this.value) || 0)">`;
            break;
        case 'select':
            const options = fieldConfig.options.map(opt => 
                `<option value="${opt}" ${fieldValue === opt ? 'selected' : ''}>${opt}</option>`
            ).join('');
            input = `<select id="${id}" onchange="updateConfigValue('${sectionKey}', '${fieldKey}', this.value)">${options}</select>`;
            break;
        case 'text':
        default:
            input = `<input type="text" id="${id}" value="${fieldValue}" ${placeholder ? `placeholder="${placeholder}"` : ''} onchange="updateConfigValue('${sectionKey}', '${fieldKey}', this.value)">`;
            break;
    }
    
    // Don't add "(Requires server restart)" to tooltip if section-level badge is shown
    const tooltipText = fieldConfig.description ? 
        (skipFieldLevelBadge && requiresRestart(sectionKey, fieldKey) ? 
            fieldConfig.description.replace(/\. Requires restart\.?$/i, '').replace(/\(Requires server restart\)/gi, '').trim() :
            fieldConfig.description) : '';
    
    const tooltip = tooltipText ? `
        <span class="config-field-tooltip">
            <svg class="config-field-tooltip-icon" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                <circle cx="12" cy="12" r="10"></circle>
                <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"></path>
                <line x1="12" y1="17" x2="12.01" y2="17"></line>
            </svg>
            <span class="config-field-tooltip-text">${tooltipText}</span>
        </span>
    ` : '';
    
    // Skip field-level restart indicator if section-level badge is shown (all fields require restart)
    const restartIndicator = requiresRestart(sectionKey, fieldKey) && !skipFieldLevelBadge ? 
        '<span style="background: #f59e0b; color: white; padding: 2px 6px; border-radius: 3px; font-size: 10px; font-weight: 600; margin-left: 6px;">⚠️ Restart</span>' : '';
    
    return `
        <div class="config-form-field">
            <label for="${id}" class="config-field-label">
                ${fieldConfig.label}${restartIndicator}
                ${tooltip}
            </label>
            ${input}
        </div>
    `;
}

function toggleConfigSection(sectionKey, enabled) {
    const section = document.querySelector(`[data-section="${sectionKey}"]`);
    const content = section.querySelector('.config-form-section-content');
    content.style.display = enabled ? 'block' : 'none';
    
    if (!enabled) {
        // Remove section from config
        if (currentConfig[sectionKey]) {
            delete currentConfig[sectionKey];
            updateJsonEditor();
        }
    } else if (!currentConfig[sectionKey]) {
        // Initialize empty section
        currentConfig[sectionKey] = {};
        updateJsonEditor();
    }
}

function updateConfigValue(sectionKey, fieldKey, value) {
    if (!currentConfig[sectionKey]) {
        currentConfig[sectionKey] = {};
    }
    currentConfig[sectionKey][fieldKey] = value;
    
    // When error_type changes, automatically update status_code based on error type
    // (status_code is deprecated but kept for backward compatibility)
    if (sectionKey === 'errors' && fieldKey === 'error_type') {
        let statusCode = 429; // default
        switch (value) {
            case 'rate_limit':
                statusCode = 429;
                break;
            case 'server_error':
                statusCode = 500;
                break;
            case 'unauthorized':
                statusCode = 401;
                break;
        }
        currentConfig[sectionKey].status_code = statusCode;
    }
    
    updateJsonEditor();
}

function updateJsonEditor() {
    document.getElementById('configJsonTextarea').value = JSON.stringify(currentConfig, null, 2);
}

function cancelConfigEdit() {
    configEditMode = false;
    document.getElementById('configContent').style.display = 'block';
    document.getElementById('configEditor').style.display = 'none';
    document.getElementById('toggleConfigViewText').textContent = 'Edit Mode';
    if (currentConfig) {
        document.getElementById('configContent').innerHTML = renderConfig(currentConfig);
    }
}

async function saveConfig() {
    try {
        // If JSON editor is active, parse JSON first
        const jsonTab = document.getElementById('configTabJson');
        if (jsonTab.classList.contains('active')) {
            const jsonText = document.getElementById('configJsonTextarea').value;
            try {
                currentConfig = JSON.parse(jsonText);
            } catch (e) {
                showToast(`Invalid JSON: ${e.message}`, 'error');
                return;
            }
        }
        
        const response = await fetch(`${API_BASE_URL}/config/save`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(currentConfig)
        });
        
        const result = await response.json();
        
        if (response.ok) {
            showToast('Configuration saved successfully!', 'success');
            // Reload config
            await loadConfigData();
            cancelConfigEdit();
        } else {
            showToast(`Failed to save configuration: ${result.error || result.detail || 'Unknown error'}`, 'error');
        }
    } catch (error) {
        showToast(`Failed to save configuration: ${error.message}`, 'error');
    }
}

function renderConfig(config) {
    if (!config || Object.keys(config).length === 0) {
        return '<div class="empty-state">No configuration available</div>';
    }
    
    let html = '';
    
    if (config.rate_limit) {
        html += `
            <div class="config-section">
                <h3>Rate Limiting</h3>
                <div class="config-item">
                    <span class="config-label">Enabled</span>
                    <span class="config-value">${config.rate_limit.enabled !== false ? 'Yes' : 'No'}</span>
                </div>
                <div class="config-item">
                    <span class="config-label">Limit</span>
                    <span class="config-value">${config.rate_limit.limit || 15}</span>
                </div>
                <div class="config-item">
                    <span class="config-label">Window (seconds)</span>
                    <span class="config-value">${config.rate_limit.window_sec || 900}</span>
                </div>
            </div>
        `;
    }
    
    if (config.persistence) {
        html += `
            <div class="config-section">
                <h3>State Persistence</h3>
                <div class="config-item">
                    <span class="config-label">Enabled</span>
                    <span class="config-value">${config.persistence.enabled !== false ? 'Yes' : 'No'}</span>
                </div>
                <div class="config-item">
                    <span class="config-label">File Path</span>
                    <span class="config-value">${config.persistence.file_path || '~/.playground/state.json'}</span>
                </div>
                <div class="config-item">
                    <span class="config-label">Auto-save</span>
                    <span class="config-value">${config.persistence.auto_save !== false ? 'Yes' : 'No'}</span>
                </div>
                <div class="config-item">
                    <span class="config-label">Save Interval (seconds)</span>
                    <span class="config-value">${config.persistence.save_interval || 60}</span>
                </div>
            </div>
        `;
    }
    
    if (config.auth) {
        html += `
            <div class="config-section">
                <h3>Authentication</h3>
                <div class="config-item">
                    <span class="config-label">Validation Disabled</span>
                    <span class="config-value">${config.auth.disable_validation ? 'Yes' : 'No'}</span>
                </div>
                <div class="config-item">
                    <span class="config-label">Status</span>
                    <span class="config-value">${config.auth.disable_validation ? 'Disabled (testing mode)' : 'Enabled (enforces auth like real API)'}</span>
                </div>
            </div>
        `;
    }
    
    if (config.streaming) {
        html += `
            <div class="config-section">
                <h3>Streaming</h3>
                <div class="config-item">
                    <span class="config-label">Default Delay (ms)</span>
                    <span class="config-value">${config.streaming.default_delay_ms || 100}</span>
                </div>
            </div>
        `;
    }
    
    if (config.errors) {
        // Determine status code from error type for display
        let statusCode = 429;
        const errorType = config.errors.error_type || 'rate_limit';
        switch (errorType) {
            case 'rate_limit':
                statusCode = 429;
                break;
            case 'server_error':
                statusCode = 500;
                break;
            case 'unauthorized':
                statusCode = 401;
                break;
        }
        
        html += `
            <div class="config-section">
                <h3>Error Simulation</h3>
                <div class="config-item">
                    <span class="config-label">Enabled</span>
                    <span class="config-value">${config.errors.enabled ? 'Yes' : 'No'}</span>
                </div>
                ${config.errors.enabled ? `
                <div class="config-item">
                    <span class="config-label">Error Rate</span>
                    <span class="config-value">${(config.errors.error_rate || 0) * 100}%</span>
                </div>
                <div class="config-item">
                    <span class="config-label">Error Type</span>
                    <span class="config-value">${errorType}</span>
                </div>
                <div class="config-item">
                    <span class="config-label">Status Code</span>
                    <span class="config-value">${statusCode} (auto)</span>
                </div>
                ` : ''}
            </div>
        `;
    }
    
    // Seeding Configuration Sections
    if (config.tweets) {
        const tweetCount = config.tweets.texts ? config.tweets.texts.length : 0;
        html += `
            <div class="config-section">
                <h3>Seeding: Tweets</h3>
                <div class="config-item">
                    <span class="config-label">Custom Tweet Texts</span>
                    <span class="config-value">${tweetCount} ${tweetCount === 1 ? 'text' : 'texts'}</span>
                </div>
            </div>
        `;
    }
    
    if (config.users) {
        const userCount = config.users.profiles ? config.users.profiles.length : 0;
        html += `
            <div class="config-section">
                <h3>Seeding: Users</h3>
                <div class="config-item">
                    <span class="config-label">Custom User Profiles</span>
                    <span class="config-value">${userCount} ${userCount === 1 ? 'profile' : 'profiles'}</span>
                </div>
            </div>
        `;
    }
    
    if (config.places) {
        const placeCount = config.places.places ? config.places.places.length : 0;
        html += `
            <div class="config-section">
                <h3>Seeding: Places</h3>
                <div class="config-item">
                    <span class="config-label">Custom Places</span>
                    <span class="config-value">${placeCount} ${placeCount === 1 ? 'place' : 'places'}</span>
                </div>
            </div>
        `;
    }
    
    if (config.topics) {
        const topicCount = config.topics.topics ? config.topics.topics.length : 0;
        html += `
            <div class="config-section">
                <h3>Seeding: Topics</h3>
                <div class="config-item">
                    <span class="config-label">Custom Topics</span>
                    <span class="config-value">${topicCount} ${topicCount === 1 ? 'topic' : 'topics'}</span>
                </div>
            </div>
        `;
    }
    
    if (config.seeding) {
        html += `
            <div class="config-section">
                <h3>Seeding Configuration</h3>
        `;
        
        if (config.seeding.users) {
            html += `
                <div class="config-item">
                    <span class="config-label">User Seeding</span>
                    <span class="config-value">${config.seeding.users.count || 'default'} users</span>
                </div>
            `;
        }
        
        if (config.seeding.tweets) {
            html += `
                <div class="config-item">
                    <span class="config-label">Tweet Seeding</span>
                    <span class="config-value">${config.seeding.tweets.count || 'default'} tweets</span>
                </div>
            `;
        }
        
        html += `</div>`;
    }
    
    // Show any other truly unknown sections
    const handledSections = ['rate_limit', 'persistence', 'auth', 'streaming', 'errors', 'tweets', 'users', 'places', 'topics', 'seeding'];
    const otherSections = Object.keys(config).filter(key => !handledSections.includes(key));
    
    if (otherSections.length > 0) {
        html += `
            <div class="config-section">
                <h3>Other Configuration</h3>
                <div class="config-item">
                    <span class="config-label">Sections</span>
                    <span class="config-value">${otherSections.join(', ')}</span>
                </div>
            </div>
        `;
    }
    
    if (!html) {
        html = '<div class="empty-state">Configuration loaded but no sections to display</div>';
    }
    
    return html;
}

// State Management
async function loadStateInfo() {
    try {
        const response = await fetch(`${API_BASE_URL}/state/export`);
        const data = await response.json();
        
        const statsGrid = document.getElementById('stateStatsGrid');
        if (!statsGrid) return;
        
        // Ensure the grid has the proper CSS class
        if (!statsGrid.classList.contains('state-stats-grid')) {
            statsGrid.classList.add('state-stats-grid');
        }
        
        const userCount = Object.keys(data.users || {}).length;
        const tweetCount = Object.keys(data.tweets || {}).length;
        const listCount = Object.keys(data.lists || {}).length;
        const spaceCount = Object.keys(data.spaces || {}).length;
        const communityCount = Object.keys(data.communities || {}).length;
        const mediaCount = Object.keys(data.media || {}).length;
        const dmCount = Object.keys(data.dm_conversations || {}).length;
        const dmEventCount = Object.keys(data.dm_events || {}).length;
        const streamRulesCount = Object.keys(data.search_stream_rules || {}).length;
        const newsCount = Object.keys(data.news || {}).length;
        
        // Calculate relationship counts
        const relationships = extractRelationships(data, 'all');
        const relationshipCount = relationships.length;
        const bookmarkCount = relationships.filter(r => r.type === 'bookmark').length;
        const likeCount = relationships.filter(r => r.type === 'like').length;
        const followingCount = relationships.filter(r => r.type === 'following').length;
        const repostCount = relationships.filter(r => r.type === 'retweet').length;
        
        const statsHtml = `
            <div class="state-stat-item">
                <div class="state-stat-label">Users</div>
                <div class="state-stat-value">${userCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Posts</div>
                <div class="state-stat-value">${tweetCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Lists</div>
                <div class="state-stat-value">${listCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Spaces</div>
                <div class="state-stat-value">${spaceCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Communities</div>
                <div class="state-stat-value">${communityCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Media</div>
                <div class="state-stat-value">${mediaCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">News</div>
                <div class="state-stat-value">${newsCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">DM Conversations</div>
                <div class="state-stat-value">${dmCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">DM Messages</div>
                <div class="state-stat-value">${dmEventCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Stream Rules</div>
                <div class="state-stat-value">${streamRulesCount}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Relationships</div>
                <div class="state-stat-value">${relationshipCount}</div>
            </div>
            <div class="state-stat-item" style="font-size: 11px; opacity: 0.8;">
                <div class="state-stat-label">Bookmarks</div>
                <div class="state-stat-value">${bookmarkCount}</div>
            </div>
            <div class="state-stat-item" style="font-size: 11px; opacity: 0.8;">
                <div class="state-stat-label">Likes</div>
                <div class="state-stat-value">${likeCount}</div>
            </div>
            <div class="state-stat-item" style="font-size: 11px; opacity: 0.8;">
                <div class="state-stat-label">Following</div>
                <div class="state-stat-value">${followingCount}</div>
            </div>
            <div class="state-stat-item" style="font-size: 11px; opacity: 0.8;">
                <div class="state-stat-label">Reposts</div>
                <div class="state-stat-value">${repostCount}</div>
            </div>
        `;
        
        statsGrid.innerHTML = statsHtml;
        
        // Update "Last Updated" timestamp under State Actions button
        const lastUpdatedElement = document.getElementById('stateLastUpdated');
        if (lastUpdatedElement) {
            lastUpdatedElement.textContent = `Last updated: ${formatDate(data.exported_at)}`;
        }
        
        // Also update detailed state grid if it exists
        const detailedGrid = document.getElementById('stateStatsGridDetailed');
        if (detailedGrid) {
            // Ensure the grid has the proper CSS class
            if (!detailedGrid.classList.contains('state-stats-grid')) {
                detailedGrid.classList.add('state-stats-grid');
            }
            detailedGrid.innerHTML = statsHtml;
        }
    } catch (error) {
        const errorHtml = `<div style="color: var(--danger); grid-column: 1 / -1;">Failed to load state info: ${error.message}</div>`;
        const errorStatsGrid = document.getElementById('stateStatsGrid');
        if (errorStatsGrid) {
            errorStatsGrid.innerHTML = errorHtml;
        }
        const errorDetailedGrid = document.getElementById('stateStatsGridDetailed');
        if (errorDetailedGrid) {
            errorDetailedGrid.innerHTML = errorHtml;
        }
    }
}

async function resetState() {
    try {
        const confirmed = await showConfirm('Reset State', 'Are you sure you want to reset the state? This will delete all current data and restore initial seeded data.');
        if (!confirmed) {
            return;
        }
        
        const response = await fetch(`${API_BASE_URL}/state/reset`, {
            method: 'POST'
        });
        
        if (response.ok) {
            showToast('State reset successfully!', 'success');
            // Reload state info and explorer data
            await loadStateInfo();
            loadExplorerData();
            loadExplorer();
        } else {
            const error = await response.text();
            showToast(`Failed to reset state: ${error}`, 'error');
        }
    } catch (error) {
        showToast(`Failed to reset state: ${error.message}`, 'error');
    }
}

async function deleteState() {
    try {
        const confirmed = await showConfirm('Delete State', 'Are you sure you want to delete all state? This will permanently delete all data and leave the playground empty. This action cannot be undone.');
        if (!confirmed) {
            return;
        }
        
        const response = await fetch(`${API_BASE_URL}/state`, {
            method: 'DELETE'
        });
        
        if (response.ok) {
            showToast('State deleted successfully!', 'success');
            // Reload state info and explorer data
            await loadStateInfo();
            loadExplorerData();
            loadExplorer();
        } else {
            const error = await response.text();
            showToast(`Failed to delete state: ${error}`, 'error');
        }
    } catch (error) {
        showToast(`Failed to delete state: ${error.message}`, 'error');
    }
}

async function exportState() {
    try {
        const response = await fetch(`${API_BASE_URL}/state/export`);
        const data = await response.json();
        
        const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `playground-state-${new Date().toISOString().split('T')[0]}.json`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    } catch (error) {
        showToast(`Failed to export state: ${error.message}`, 'error');
    }
}

function handleFileSelect(event) {
    const file = event.target.files[0];
    if (!file) return;
    
    const reader = new FileReader();
    reader.onload = async (e) => {
        try {
            const data = JSON.parse(e.target.result);
            
            const response = await fetch(`${API_BASE_URL}/state/import`, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(data)
            });
            
            if (response.ok) {
                showToast('State imported successfully!', 'success');
                loadStateInfo();
                loadExplorerData();
                loadExplorer();
            } else {
                const error = await response.text();
                showToast(`Failed to import state: ${error}`, 'error');
            }
        } catch (error) {
            showToast(`Failed to parse file: ${error.message}`, 'error');
        }
    };
    reader.readAsText(file);
}

async function saveState() {
    try {
        const response = await fetch(`${API_BASE_URL}/state/save`, {
            method: 'POST'
        });
        
        if (response.ok) {
            showToast('State saved successfully!', 'success');
        } else {
            const error = await response.text();
            showToast(`Failed to save state: ${error}`, 'error');
        }
    } catch (error) {
        showToast(`Failed to save state: ${error.message}`, 'error');
    }
}

// Utilities
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatDate(dateString) {
    if (!dateString) return 'N/A';
    try {
        const date = new Date(dateString);
        return date.toLocaleString();
    } catch (e) {
        return dateString;
    }
}

function formatTime(timestamp) {
    const date = new Date(timestamp);
    const now = new Date();
    const diff = now - date;
    
    if (diff < 60000) return 'Just now';
    if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
    if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
    return date.toLocaleDateString();
}

function toggleStateMenu() {
    const menu = document.getElementById('stateMenu');
    if (menu.style.display === 'none' || !menu.style.display) {
        menu.style.display = 'block';
        // Close menu when clicking outside
        setTimeout(() => {
            document.addEventListener('click', function closeMenu(e) {
                if (!menu.contains(e.target) && !e.target.closest('.dropdown-menu-container')) {
                    menu.style.display = 'none';
                    document.removeEventListener('click', closeMenu);
                }
            });
        }, 0);
    } else {
        menu.style.display = 'none';
    }
}

// Streaming support (unified with Request Builder)
let currentStreamController = null;
let currentStreamItem = null; // Track current stream for history updates

async function startStream() {
    const endpointSelect = document.getElementById('requestEndpoint');
    const endpoint = endpointSelect.value.trim();
    const auth = getAuthHeader('request');
    
    if (!endpoint) {
        showToast('Please select a streaming endpoint', 'warning');
        return;
    }
    
    // Stop any existing stream
    if (currentStreamController) {
        stopStream();
    }
    
    // Get endpoint info for path parameter handling
    const selectedOption = endpointSelect.options[endpointSelect.selectedIndex];
    const method = selectedOption ? (selectedOption.dataset.method || 'GET') : 'GET';
    const parameters = selectedOption ? JSON.parse(selectedOption.dataset.parameters || '[]') : [];
    const pathParamNames = new Set();
    parameters.filter(p => p.in === 'path').forEach(p => pathParamNames.add(p.name));
    
    // Collect all parameters for history
    const allParams = [];
    document.querySelectorAll('#queryParamsContainer .param-row').forEach(row => {
        const keyInput = row.querySelector('.param-key');
        const valueInput = row.querySelector('.param-value');
        if (keyInput && valueInput) {
            const key = keyInput.value.trim();
            const value = valueInput.value.trim();
            if (key && value) {
                allParams.push({ key, value, isPath: pathParamNames.has(key) });
            }
        }
    });
    
    // Replace path parameters if needed
    let finalEndpoint = endpoint;
    allParams.forEach(p => {
        if (p.isPath) {
            finalEndpoint = finalEndpoint.replace(`{${p.key}}`, encodeURIComponent(p.value));
        }
    });
    
    // Build URL with query parameters (excluding path params)
    const queryParams = allParams.filter(p => !p.isPath).map(p => `${encodeURIComponent(p.key)}=${encodeURIComponent(p.value)}`);
    const queryString = queryParams.length > 0 ? '?' + queryParams.join('&') : '';
    const url = `${API_BASE_URL}${finalEndpoint}${queryString}`;
    
    // Record stream start in unified request history
    currentStreamItem = {
        id: Date.now(),
        timestamp: new Date().toISOString(),
        method: method,
        endpoint: endpoint,
        fullUrl: url,
        auth: auth,
        parameters: allParams,
        status: 'connecting',
        messageCount: 0,
        duration: 0,
        isStream: true
    };
    
    // Add to unified request history
    requestHistory.unshift({
        method: method,
        endpoint: endpoint,
        fullUrl: url,
        status: 0, // Will be updated when stream ends
        statusText: 'Streaming',
        timestamp: new Date().toISOString(),
        requestHeaders: { 'Authorization': auth || '' },
        requestBody: null,
        queryParams: allParams.filter(p => !p.isPath).map(p => `${p.key}=${p.value}`),
        responseHeaders: {},
        responseBody: null,
        isStream: true,
        streamStatus: 'connecting',
        messageCount: 0,
        duration: 0,
        streamItemId: currentStreamItem.id
    });
    if (requestHistory.length > 100) {
        requestHistory = requestHistory.slice(0, 100);
    }
    localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
    updateRequestHistory();
    
    // Update UI
    document.getElementById('sendRequestBtn').style.display = 'none';
    document.getElementById('stopStreamBtn').style.display = 'inline-flex';
    const streamStatusEl = document.getElementById('responseStatus');
    streamStatusEl.textContent = 'Connecting...';
    streamStatusEl.className = 'status-badge warning';
    
    const streamContent = document.getElementById('streamContent');
    streamContent.innerHTML = '<div class="stream-message">Connecting to stream...</div>';
    
    // Create AbortController for cancellation
    currentStreamController = new AbortController();
    const streamStartTime = Date.now();
    let messageCount = 0; // Declare outside try block so it's accessible in catch
    
    try {
        const headers = {
            'Authorization': auth || 'Bearer test',
        };
        
        // Add X-Auth-Method header to help backend distinguish between Bearer Token (App-Only) 
        // and OAuth 2.0 User Context (both use "Bearer" format)
        if (auth) {
            const authMethod = document.getElementById('requestAuthMethod').value;
            if (authMethod === 'bearer') {
                headers['X-Auth-Method'] = 'BearerToken';
            } else if (authMethod === 'oauth1') {
                headers['X-Auth-Method'] = 'OAuth1a';
            } else if (authMethod === 'oauth2') {
                headers['X-Auth-Method'] = 'OAuth2User';
            }
        }
        
        const response = await fetch(url, {
            method: 'GET',
            headers: headers,
            signal: currentStreamController.signal,
        });
        
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        // Update status in unified history
        if (currentStreamItem) {
            currentStreamItem.status = 'streaming';
            currentStreamItem.startTime = new Date().toISOString();
            // Update unified request history
            const historyIndex = requestHistory.findIndex(h => h.isStream && h.streamItemId === currentStreamItem.id);
            if (historyIndex !== -1) {
                requestHistory[historyIndex].streamStatus = 'streaming';
                localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
                updateRequestHistory();
            }
        }
        
        streamStatusEl.textContent = 'Streaming';
        streamStatusEl.className = 'status-badge success';
        
        // Clear content and start displaying stream
        streamContent.innerHTML = '<div class="stream-messages"></div>';
        const messagesContainer = streamContent.querySelector('.stream-messages');
        
        // Read the stream
        // Backend uses Server-Sent Events (SSE) format: "data: <json>\n\n"
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        
        while (true) {
            const { done, value } = await reader.read();
            
            if (done) {
                // Process any remaining buffer
                if (buffer.trim()) {
                    const lines = buffer.split('\n');
                    for (const line of lines) {
                        const trimmed = line.trim();
                        if (trimmed.startsWith('data: ')) {
                            const jsonStr = trimmed.substring(6); // Remove "data: " prefix
                            if (jsonStr) {
                                messageCount++;
                                let displayText;
                                try {
                                    const parsed = JSON.parse(jsonStr);
                                    displayText = JSON.stringify(parsed, null, 2);
                                } catch (e) {
                                    displayText = jsonStr;
                                }
                                
                                const messageDiv = document.createElement('div');
                                messageDiv.className = 'stream-message-item';
                                messageDiv.innerHTML = `
                                    <div class="stream-message-header">
                                        <span class="stream-message-number">#${messageCount}</span>
                                        <span class="stream-message-time">${new Date().toLocaleTimeString()}</span>
                                    </div>
                                    <pre class="stream-message-content">${syntaxHighlight(displayText)}</pre>
                                `;
                                messagesContainer.appendChild(messageDiv);
                                streamContent.scrollTop = streamContent.scrollHeight;
                            }
                        }
                    }
                }
                break;
            }
            
            // Decode chunk and add to buffer
            buffer += decoder.decode(value, { stream: true });
            
            // Process SSE format: "data: <json>\n\n"
            // Look for complete SSE events (ending with \n\n)
            while (true) {
                const eventEnd = buffer.indexOf('\n\n');
                if (eventEnd === -1) {
                    // No complete event yet, wait for more data
                    break;
                }
                
                // Extract the event (including the \n\n)
                const event = buffer.substring(0, eventEnd);
                buffer = buffer.substring(eventEnd + 2); // Remove event and \n\n
                
                // Process each line in the event
                const lines = event.split('\n');
                for (const line of lines) {
                    const trimmed = line.trim();
                    if (trimmed.startsWith('data: ')) {
                        const jsonStr = trimmed.substring(6); // Remove "data: " prefix
                        if (jsonStr) {
                            messageCount++;
                            let displayText;
                            try {
                                // Parse the JSON
                                const parsed = JSON.parse(jsonStr);
                                // Pretty-print for display
                                displayText = JSON.stringify(parsed, null, 2);
                            } catch (e) {
                                // Not valid JSON, display as-is
                                displayText = jsonStr;
                            }
                            
                            // Add message to display
                            const messageDiv = document.createElement('div');
                            messageDiv.className = 'stream-message-item';
                            messageDiv.innerHTML = `
                                <div class="stream-message-header">
                                    <span class="stream-message-number">#${messageCount}</span>
                                    <span class="stream-message-time">${new Date().toLocaleTimeString()}</span>
                                </div>
                                <pre class="stream-message-content">${syntaxHighlight(displayText)}</pre>
                            `;
                            messagesContainer.appendChild(messageDiv);
                            
                            // Update message count in unified history
                            if (currentStreamItem) {
                                currentStreamItem.messageCount = messageCount;
                                // Find and update the corresponding history item
                                const historyIndex = requestHistory.findIndex(h => h.isStream && h.streamItemId === currentStreamItem.id);
                                if (historyIndex !== -1) {
                                    requestHistory[historyIndex].messageCount = messageCount;
                                    requestHistory[historyIndex].streamStatus = 'active';
                                    localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
                                }
                            }
                            
                            // Auto-scroll to bottom
                            streamContent.scrollTop = streamContent.scrollHeight;
            }
        }
                }
            }
        }
        
        // Stream ended
        const duration = Date.now() - streamStartTime;
        if (currentStreamItem) {
            currentStreamItem.status = 'ended';
            currentStreamItem.messageCount = messageCount;
            currentStreamItem.duration = duration;
            currentStreamItem.endTime = new Date().toISOString();
            
            // Update unified request history
            const historyIndex = requestHistory.findIndex(h => h.isStream && h.streamItemId === currentStreamItem.id);
            if (historyIndex !== -1) {
                requestHistory[historyIndex].status = 200;
                requestHistory[historyIndex].statusText = 'Stream Ended';
                requestHistory[historyIndex].messageCount = messageCount;
                requestHistory[historyIndex].duration = duration;
                requestHistory[historyIndex].endTime = currentStreamItem.endTime;
                requestHistory[historyIndex].streamStatus = 'ended';
                localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
                updateRequestHistory();
            }
        }
        
        const responseStatus = document.getElementById('responseStatus');
        responseStatus.textContent = 'Stream Ended';
        responseStatus.className = 'status-badge success';
        currentStreamController = null;
        currentStreamItem = null;
        document.getElementById('sendRequestBtn').style.display = 'inline-flex';
        document.getElementById('stopStreamBtn').style.display = 'none';
        
    } catch (error) {
        const duration = Date.now() - streamStartTime;
        if (currentStreamItem) {
            currentStreamItem.status = error.name === 'AbortError' ? 'stopped' : 'error';
            currentStreamItem.messageCount = messageCount;
            currentStreamItem.duration = duration;
            currentStreamItem.endTime = new Date().toISOString();
            currentStreamItem.error = error.message;
            
            // Update unified request history
            const historyIndex = requestHistory.findIndex(h => h.isStream && h.streamItemId === currentStreamItem.id);
            if (historyIndex !== -1) {
                requestHistory[historyIndex].status = error.name === 'AbortError' ? 0 : 0;
                requestHistory[historyIndex].statusText = error.name === 'AbortError' ? 'Stopped' : 'Error';
                requestHistory[historyIndex].messageCount = messageCount;
                requestHistory[historyIndex].duration = duration;
                requestHistory[historyIndex].endTime = currentStreamItem.endTime;
                requestHistory[historyIndex].error = error.message;
                requestHistory[historyIndex].streamStatus = error.name === 'AbortError' ? 'stopped' : 'error';
                localStorage.setItem('requestHistory', JSON.stringify(requestHistory));
                updateRequestHistory();
            }
        }
        
        const responseStatus = document.getElementById('responseStatus');
        const streamContent = document.getElementById('streamContent');
        if (error.name === 'AbortError') {
            responseStatus.textContent = 'Stopped';
            responseStatus.className = 'status-badge warning';
            if (streamContent) {
                streamContent.innerHTML += '<div class="stream-message" style="color: var(--text-muted);">Stream stopped by user</div>';
            }
        } else {
            responseStatus.textContent = 'Error';
            responseStatus.className = 'status-badge error';
            if (streamContent) {
                streamContent.innerHTML = `<div class="stream-message" style="color: var(--danger);">Stream error: ${escapeHtml(error.message)}</div>`;
            }
        }
        currentStreamController = null;
        document.getElementById('sendRequestBtn').style.display = 'inline-flex';
        document.getElementById('stopStreamBtn').style.display = 'none';
    }
}

function stopStream() {
    if (currentStreamController) {
        currentStreamController.abort();
        currentStreamController = null;
    }
}

function clearStreamOutput() {
    const streamContent = document.getElementById('streamContent');
    if (streamContent) {
        streamContent.innerHTML = '<div class="empty-state">Select a streaming endpoint and click "Start Stream" to begin</div>';
    }
}

function formatDuration(ms) {
    if (ms < 1000) return `${ms}ms`;
    if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`;
    const minutes = Math.floor(ms / 60000);
    const seconds = Math.floor((ms % 60000) / 1000);
    return `${minutes}m ${seconds}s`;
}

// Show search operators tooltip
function showSearchOperatorsTooltip(event) {
    const tooltip = document.getElementById('searchOperatorsTooltip');
    if (!tooltip) return;
    
    const typeSelect = document.getElementById('unifiedExplorerType');
    const typeFilter = typeSelect?.value || 'users';
    updateSearchOperatorsTooltip(typeFilter);
    
    tooltip.style.display = 'block';
}

// Hide search operators tooltip
function hideSearchOperatorsTooltip() {
    const tooltip = document.getElementById('searchOperatorsTooltip');
    if (tooltip) {
        tooltip.style.display = 'none';
    }
}

// Update search operators tooltip content based on entity type
function updateSearchOperatorsTooltip(type) {
    const content = document.getElementById('searchOperatorsContent');
    if (!content) return;
    
    let operators = [];
    
    switch (type) {
        case 'relationships':
            operators = [
                { op: 'user:', desc: 'Filter by user (username or name)' },
                { op: 'username:', desc: 'Filter by username' },
                { op: 'post:', desc: 'Filter by post ID' },
                { op: 'tweet:', desc: 'Filter by tweet ID (alias for post:)' },
                { op: 'list:', desc: 'Filter by list ID' },
                { op: 'type:', desc: 'Filter by relationship type (bookmark, like, following, etc.)' },
                { op: 'id:', desc: 'Filter by relationship ID' }
            ];
            break;
        case 'tweets':
            operators = [
                { op: 'user:', desc: 'Filter by author (username or name)' },
                { op: 'username:', desc: 'Filter by author username' },
                { op: 'post:', desc: 'Filter by post ID' },
                { op: 'tweet:', desc: 'Filter by tweet ID (alias for post:)' },
                { op: 'id:', desc: 'Filter by tweet ID' }
            ];
            break;
        case 'lists':
            operators = [
                { op: 'list:', desc: 'Filter by list ID' },
                { op: 'id:', desc: 'Filter by list ID' }
            ];
            break;
        default:
            operators = [
                { op: 'id:', desc: 'Filter by ID' }
            ];
    }
    
    if (operators.length === 0) {
        content.innerHTML = '<div style="color: var(--text-muted);">No special operators available. Use regular text search.</div>';
        return;
    }
    
    content.innerHTML = operators.map(op => 
        `<div style="margin-bottom: 6px;"><code style="color: var(--primary); font-weight: 600;">${escapeHtml(op.op)}</code> <span style="color: var(--text-secondary);">${escapeHtml(op.desc)}</span></div>`
    ).join('') + '<div style="margin-top: 8px; padding-top: 8px; border-top: 1px solid var(--border); font-size: 12px; color: var(--text-muted);">You can combine multiple operators and add free text search.</div>';
}

// Usage
let creditsCharts = {};

async function loadCredits() {
    const creditsContent = document.getElementById('creditsContent');
    if (!creditsContent) return;
    
    creditsContent.innerHTML = '<div class="loading">Loading usage data...</div>';
    
    try {
        // Get account ID (default to "0" for playground user)
        const accountID = '0';
        
        // Fetch cost breakdown, usage, and pricing in parallel
        const [costResponse, usageEventResponse, usageRequestResponse, pricingResponse] = await Promise.all([
            fetch(`${API_BASE_URL}/api/accounts/${accountID}/cost`),
            fetch(`${API_BASE_URL}/api/accounts/${accountID}/usage?interval=30days&groupBy=eventType`),
            fetch(`${API_BASE_URL}/api/accounts/${accountID}/usage?interval=30days&groupBy=requestType`),
            fetch(`${API_BASE_URL}/api/credits/pricing`)
        ]);
        
        if (!costResponse.ok || !usageEventResponse.ok || !usageRequestResponse.ok || !pricingResponse.ok) {
            throw new Error('Failed to load credits data');
        }
        
        const costData = await costResponse.json();
        const usageEventData = await usageEventResponse.json();
        const usageRequestData = await usageRequestResponse.json();
        const pricingData = await pricingResponse.json();
        
        renderCreditsPage(costData, usageEventData, usageRequestData, pricingData);
    } catch (error) {
        console.error('Error loading credits:', error);
        creditsContent.innerHTML = `<div class="error-state">Error loading usage data: ${error.message}</div>`;
    }
}

function renderCreditsPage(costData, usageEventData, usageRequestData, pricingData) {
    const creditsContent = document.getElementById('creditsContent');
    if (!creditsContent) return;
    
    const totalCost = costData.totalCost || 0;
    const eventTypeCosts = costData.eventTypeCosts || [];
    const requestTypeCosts = costData.requestTypeCosts || [];
    const eventTypeTimeSeries = costData.eventTypeTimeSeries || [];
    const requestTypeTimeSeries = costData.requestTypeTimeSeries || [];
    // Format billing cycle start date consistently with chart dates
    // Parse the RFC3339 date and format it to match the chart x-axis format
    let billingCycleStart = 'N/A';
    if (costData.billingCycleStart) {
        const startDate = new Date(costData.billingCycleStart);
        // Use the same format as chart labels for consistency
        billingCycleStart = startDate.toLocaleDateString('en-US', { month: 'numeric', day: 'numeric', year: 'numeric' });
    }
    const currentBillingCycle = costData.currentBillingCycle || 1;
    
    // Calculate totals
    const totalEventTypeUsage = eventTypeCosts.reduce((sum, item) => sum + item.usage, 0);
    const totalRequestTypeUsage = requestTypeCosts.reduce((sum, item) => sum + item.usage, 0);
    const totalEventTypeCost = eventTypeCosts.reduce((sum, item) => sum + item.totalCost, 0);
    const totalRequestTypeCost = requestTypeCosts.reduce((sum, item) => sum + item.totalCost, 0);
    
    // Get all available types for filters
    const allEventTypes = [...new Set(eventTypeCosts.map(c => c.type))];
    const allRequestTypes = [...new Set(requestTypeCosts.map(c => c.type))];
    
    creditsContent.innerHTML = `
        <!-- Stats Overview -->
        <div class="state-stats-grid" style="margin-bottom: 24px;">
            <div class="state-stat-item">
                <div class="state-stat-label">Total Estimated Cost</div>
                <div class="state-stat-value" style="color: var(--primary);">$${totalCost.toFixed(3)}</div>
                <div class="state-stat-value-small" style="margin-top: 4px;">Billing Cycle ${currentBillingCycle}</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Event Types Cost</div>
                <div class="state-stat-value">$${totalEventTypeCost.toFixed(3)}</div>
                <div class="state-stat-value-small" style="margin-top: 4px;">${totalEventTypeUsage.toLocaleString()} resources</div>
            </div>
            <div class="state-stat-item">
                <div class="state-stat-label">Request Types Cost</div>
                <div class="state-stat-value">$${totalRequestTypeCost.toFixed(3)}</div>
                <div class="state-stat-value-small" style="margin-top: 4px;">${totalRequestTypeUsage.toLocaleString()} requests</div>
            </div>
        </div>
        
        <!-- Charts -->
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-bottom: 24px;">
            <div class="explorer-section">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px;">
                    <h3 style="font-size: 16px; font-weight: 600; color: var(--text-primary); margin: 0;">Cost by Event Type</h3>
                    <select id="eventTypeChartFilter" class="select-input" onchange="updateEventTypeChart()" style="min-width: 150px;">
                        <option value="all">All Types</option>
                        ${allEventTypes.map(type => `<option value="${type}">${type}</option>`).join('')}
                    </select>
                </div>
                <div style="font-size: 12px; color: #71767a; margin-bottom: 12px;">Billing Cycle ${currentBillingCycle} (Started: ${billingCycleStart})</div>
                <canvas id="eventTypeCostChart" style="max-height: 300px;"></canvas>
            </div>
            <div class="explorer-section">
                <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 20px;">
                    <h3 style="font-size: 16px; font-weight: 600; color: var(--text-primary); margin: 0;">Cost by Request Type</h3>
                    <select id="requestTypeChartFilter" class="select-input" onchange="updateRequestTypeChart()" style="min-width: 150px;">
                        <option value="all">All Types</option>
                        ${allRequestTypes.map(type => `<option value="${type}">${type}</option>`).join('')}
                    </select>
                </div>
                <div style="font-size: 12px; color: #71767a; margin-bottom: 12px;">Billing Cycle ${currentBillingCycle} (Started: ${billingCycleStart})</div>
                <canvas id="requestTypeCostChart" style="max-height: 300px;"></canvas>
            </div>
        </div>
        
        <!-- Breakdown Tables -->
        <div style="display: flex; flex-direction: column; gap: 24px;">
            <div class="table-container">
                <div class="table-header" style="display: flex; flex-direction: column; align-items: flex-start; gap: 0; margin-bottom: 24px;">
                    <h3 style="margin: 0 0 16px 0; font-size: 16px; font-weight: 600; color: #ffffff;">Event Type Breakdown</h3>
                    <select id="eventTypeFilter" class="select-input" onchange="filterCreditsTable('eventType')" style="min-width: 150px; margin-bottom: 0;">
                        <option value="all">All Types</option>
                        ${Object.keys(pricingData.eventTypePricing || {}).map(type => 
                            `<option value="${type}">${type}</option>`
                        ).join('')}
                    </select>
                </div>
                <div class="table-wrapper" style="overflow-x: auto; border-radius: 12px;">
                    <table class="usage-table" style="width: 100%; border-collapse: separate; border-spacing: 0; background: #000000; border-radius: 12px; overflow: hidden; border: 1px solid #2f3336;">
                        <thead style="background: linear-gradient(180deg, #202327 0%, #16181c 100%);">
                            <tr>
                                <th style="padding: 16px 20px; text-align: left; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Event Type</th>
                                <th style="padding: 16px 20px; text-align: right; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Usage</th>
                                <th style="padding: 16px 20px; text-align: right; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Price per Unit</th>
                                <th style="padding: 16px 20px; text-align: right; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Total Cost</th>
                            </tr>
                        </thead>
                        <tbody id="eventTypeTableBody">
                            ${renderCostTableRows(eventTypeCosts, 'eventType')}
                        </tbody>
                    </table>
                </div>
            </div>
            
            <div class="table-container">
                <div class="table-header" style="display: flex; flex-direction: column; align-items: flex-start; gap: 0; margin-bottom: 24px;">
                    <h3 style="margin: 0 0 16px 0; font-size: 16px; font-weight: 600; color: #ffffff;">Request Type Breakdown</h3>
                    <select id="requestTypeFilter" class="select-input" onchange="filterCreditsTable('requestType')" style="min-width: 150px; margin-bottom: 0;">
                        <option value="all">All Types</option>
                        ${Object.keys(pricingData.requestTypePricing || {}).map(type => 
                            `<option value="${type}">${type}</option>`
                        ).join('')}
                    </select>
                </div>
                <div class="table-wrapper" style="overflow-x: auto; border-radius: 12px;">
                    <table class="usage-table" style="width: 100%; border-collapse: separate; border-spacing: 0; background: #000000; border-radius: 12px; overflow: hidden; border: 1px solid #2f3336;">
                        <thead style="background: linear-gradient(180deg, #202327 0%, #16181c 100%);">
                            <tr>
                                <th style="padding: 16px 20px; text-align: left; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Request Type</th>
                                <th style="padding: 16px 20px; text-align: right; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Usage</th>
                                <th style="padding: 16px 20px; text-align: right; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Price per Unit</th>
                                <th style="padding: 16px 20px; text-align: right; font-size: 11px; font-weight: 700; color: #71767a; text-transform: uppercase; letter-spacing: 1px; border-bottom: 2px solid #2f3336; white-space: nowrap;">Total Cost</th>
                            </tr>
                        </thead>
                        <tbody id="requestTypeTableBody">
                            ${renderCostTableRows(requestTypeCosts, 'requestType')}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    `;
    
    // Store data globally for chart updates
    window.costData = costData;
    window.pricingData = pricingData;
    
    // Render charts
    updateEventTypeChart();
    updateRequestTypeChart();
    
    // Attach hover effects to table rows after rendering
    setTimeout(function() {
        attachTableHoverEffects();
    }, 100);
}

function attachTableHoverEffects() {
    const rows = document.querySelectorAll('.usage-table-row');
    rows.forEach(row => {
        row.addEventListener('mouseenter', function() {
            this.style.background = '#16181c';
            const badge = this.querySelector('.usage-type-badge');
            const cost = this.querySelector('.usage-cost');
            if (badge) {
                badge.style.background = 'rgba(120, 86, 255, 0.25)';
                badge.style.borderColor = 'rgba(120, 86, 255, 0.5)';
            }
            if (cost) {
                cost.style.background = 'rgba(120, 86, 255, 0.3)';
                cost.style.borderColor = 'rgba(120, 86, 255, 0.6)';
                cost.style.boxShadow = '0 2px 8px rgba(120, 86, 255, 0.2)';
            }
        });
        row.addEventListener('mouseleave', function() {
            this.style.background = '#000000';
            const badge = this.querySelector('.usage-type-badge');
            const cost = this.querySelector('.usage-cost');
            if (badge) {
                badge.style.background = 'rgba(120, 86, 255, 0.15)';
                badge.style.borderColor = 'rgba(120, 86, 255, 0.3)';
            }
            if (cost) {
                cost.style.background = 'rgba(120, 86, 255, 0.2)';
                cost.style.borderColor = 'rgba(120, 86, 255, 0.4)';
                cost.style.boxShadow = '0 2px 4px rgba(120, 86, 255, 0.1)';
            }
        });
    });
}

function renderCostTableRows(costs, type) {
    if (!costs || costs.length === 0) {
        return '<tr><td colspan="4" style="text-align: center; color: #71767a; padding: 48px 20px; font-size: 14px; font-style: italic;">No usage data</td></tr>';
    }
    
    return costs.map(item => `
        <tr class="usage-table-row" data-type="${item.type}" style="background: #000000; border-bottom: 1px solid #2f3336; transition: all 0.2s ease;">
            <td class="usage-type-cell" style="padding: 16px 20px; min-width: 180px;">
                <span class="usage-type-badge" style="display: inline-block; padding: 6px 14px; background: rgba(120, 86, 255, 0.15); color: #7856FF; border-radius: 6px; font-size: 13px; font-weight: 600; border: 1px solid rgba(120, 86, 255, 0.3);">${item.type}</span>
            </td>
            <td class="usage-number-cell" style="padding: 16px 20px; text-align: right; min-width: 120px;">
                <span class="usage-number" style="display: inline-block; padding: 4px 10px; background: #202327; color: #ffffff; border-radius: 4px; font-family: Monaco, Menlo, 'Courier New', monospace; font-size: 13px; font-weight: 500; border: 1px solid #2f3336;">${item.usage.toLocaleString()}</span>
            </td>
            <td class="usage-price-cell" style="padding: 16px 20px; text-align: right; min-width: 140px;">
                <span class="usage-price" style="display: inline-block; padding: 4px 10px; background: rgba(120, 86, 255, 0.1); color: #e7e9ea; border-radius: 4px; font-family: Monaco, Menlo, 'Courier New', monospace; font-size: 13px; font-weight: 500; border: 1px solid rgba(120, 86, 255, 0.2);">$${item.price.toFixed(3)}</span>
            </td>
            <td class="usage-cost-cell" style="padding: 16px 20px; text-align: right; min-width: 140px;">
                <span class="usage-cost" style="display: inline-block; padding: 6px 12px; background: rgba(120, 86, 255, 0.2); color: #7856FF; border-radius: 6px; font-size: 15px; font-weight: 700; border: 1px solid rgba(120, 86, 255, 0.4); box-shadow: 0 2px 4px rgba(120, 86, 255, 0.1);">$${item.totalCost.toFixed(3)}</span>
            </td>
        </tr>
    `).join('');
}

function updateEventTypeChart() {
    if (!window.costData) return;
    const timeSeries = window.costData.eventTypeTimeSeries || [];
    const filter = document.getElementById('eventTypeChartFilter')?.value || 'all';
    renderTimeSeriesChart('eventTypeCostChart', timeSeries, filter, 'Event Types');
}

function updateRequestTypeChart() {
    if (!window.costData) return;
    const timeSeries = window.costData.requestTypeTimeSeries || [];
    const filter = document.getElementById('requestTypeChartFilter')?.value || 'all';
    renderTimeSeriesChart('requestTypeCostChart', timeSeries, filter, 'Request Types');
}

function renderTimeSeriesChart(canvasId, timeSeries, filterType, label) {
    const canvas = document.getElementById(canvasId);
    if (!canvas) return;
    
    if (!timeSeries || timeSeries.length === 0) {
        const ctx = canvas.getContext('2d');
        ctx.fillStyle = '#71767a';
        ctx.font = '14px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('No data available', canvas.width / 2, canvas.height / 2);
        return;
    }
    
    // Destroy existing chart if it exists
    if (creditsCharts[canvasId]) {
        creditsCharts[canvasId].destroy();
    }
    
    const ctx = canvas.getContext('2d');
    
    // Extract dates and filter costs
    const labels = timeSeries.map(point => {
        const date = new Date(point.timestamp);
        return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
    });
    
    // Get all types from the time series (include any type that has costs > 0 on any day)
    const allTypes = new Set();
    timeSeries.forEach(point => {
        if (point.costs) {
            Object.keys(point.costs).forEach(type => {
                if (point.costs[type] > 0) {
                    allTypes.add(type);
                }
            });
        }
    });
    
    // Filter types if needed
    const typesToShow = filterType === 'all' 
        ? Array.from(allTypes) 
        : [filterType].filter(t => allTypes.has(t));
    
    if (typesToShow.length === 0) {
        const ctx = canvas.getContext('2d');
        ctx.fillStyle = '#71767a';
        ctx.font = '14px -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('No data available', canvas.width / 2, canvas.height / 2);
        return;
    }
    
    // Determine chart type - both cost charts use bar charts
    const chartType = (canvasId === 'eventTypeCostChart' || canvasId === 'requestTypeCostChart') ? 'bar' : 'line';
    
    // Base color palette - exact colors provided
    const baseColors = [
        { r: 120, g: 86, b: 255 },   // Purple (primary)
        { r: 29, g: 155, b: 240 },   // Blue
        { r: 255, g: 212, b: 0 },    // Yellow
        { r: 249, g: 24, b: 128 },   // Pink
        { r: 255, g: 122, b: 0 },    // Orange
        { r: 0, g: 186, b: 124 },    // Green
    ];
    
    // Function to derive a color by interpolating between base colors
    function deriveColor(index) {
        if (index < baseColors.length) {
            return baseColors[index];
        }
        
        // For indices beyond base colors, interpolate between adjacent base colors
        const baseIndex = index % baseColors.length;
        const nextBaseIndex = (baseIndex + 1) % baseColors.length;
        // Blend factor: 0.5 for first extra cycle, increasing gradually
        const cycle = Math.floor(index / baseColors.length);
        const t = 0.5 + (cycle - 1) * 0.1; // 0.5, 0.6, 0.7, 0.8, etc. (capped at reasonable values)
        
        const color1 = baseColors[baseIndex];
        const color2 = baseColors[nextBaseIndex];
        
        // Clamp t to reasonable range
        const blendFactor = Math.min(Math.max(t, 0.3), 0.9);
        
        return {
            r: Math.round(color1.r + (color2.r - color1.r) * blendFactor),
            g: Math.round(color1.g + (color2.g - color1.g) * blendFactor),
            b: Math.round(color1.b + (color2.b - color1.b) * blendFactor)
        };
    }
    
    // Function to get color as rgba string
    function getColorRGBA(index, alpha) {
        const color = deriveColor(index);
        return `rgba(${color.r}, ${color.g}, ${color.b}, ${alpha})`;
    }
    
    // Create datasets for each type
    let datasets = typesToShow.map((type, index) => {
        const backgroundColor = getColorRGBA(index, 0.8);
        const borderColor = getColorRGBA(index, 1.0);
        
        if (chartType === 'bar') {
            // Bar chart styling
            return {
                label: type,
                data: timeSeries.map(point => point.costs[type] || 0),
                backgroundColor: backgroundColor,
                borderColor: borderColor,
                borderWidth: 1,
                borderRadius: 4,
                borderSkipped: false
            };
        } else {
            // Line chart styling
            return {
                label: type,
                data: timeSeries.map(point => point.costs[type] || 0),
                borderColor: borderColor,
                backgroundColor: getColorRGBA(index, 0.1),
                borderWidth: 2,
                fill: false,
                tension: 0.4
            };
        }
    });
    
    // Always use primary purple for single dataset
    if (datasets.length === 1) {
        if (chartType === 'bar') {
            datasets[0].backgroundColor = 'rgba(120, 86, 255, 0.8)';
            datasets[0].borderColor = 'rgba(120, 86, 255, 1)';
        } else {
            datasets[0].borderColor = 'rgba(120, 86, 255, 1)';
            datasets[0].backgroundColor = 'rgba(120, 86, 255, 0.1)';
        }
    }
    
    creditsCharts[canvasId] = new Chart(ctx, {
        type: chartType,
        data: {
            labels: labels,
            datasets: datasets
        },
        options: {
            responsive: true,
            maintainAspectRatio: false,
            plugins: {
                legend: {
                    display: typesToShow.length > 1,
                    position: 'top',
                    labels: {
                        color: '#e7e9ea',
                        font: {
                            size: 12
                        },
                        padding: 12,
                        usePointStyle: chartType === 'line'
                    }
                },
                tooltip: {
                    backgroundColor: '#16181c',
                    titleColor: '#ffffff',
                    bodyColor: '#e7e9ea',
                    borderColor: '#2f3336',
                    borderWidth: 1,
                    padding: 12,
                    callbacks: {
                        title: function(context) {
                            // Show the date
                            return context[0].label;
                        },
                        label: function(context) {
                            const dataIndex = context.dataIndex;
                            const point = timeSeries[dataIndex];
                            const type = context.dataset.label;
                            const cost = context.parsed.y;
                            
                            if (cost === 0) {
                                return `${type}: $0.000`;
                            }
                            
                            // Determine if this is event type or request type pricing
                            const isEventType = canvasId === 'eventTypeCostChart';
                            const pricing = isEventType 
                                ? (window.pricingData?.eventTypePricing || {})
                                : (window.pricingData?.requestTypePricing || {});
                            
                            const pricePerItem = pricing[type] || 0;
                            const itemCount = pricePerItem > 0 ? (cost / pricePerItem) : 0;
                            const itemLabel = isEventType ? 'event' : 'request';
                            const itemLabelPlural = isEventType ? 'events' : 'requests';
                            
                            // Format the breakdown
                            let label = `${type}: $${cost.toFixed(3)}`;
                            if (pricePerItem > 0 && itemCount > 0) {
                                label += ` (${Math.round(itemCount)} ${itemCount === 1 ? itemLabel : itemLabelPlural} × $${pricePerItem.toFixed(3)}/${itemLabel})`;
                            }
                            
                            return label;
                        },
                        footer: function(context) {
                            // Calculate and show total cost for all types at this data point
                            let totalCost = 0;
                            context.forEach(item => {
                                totalCost += item.parsed.y;
                            });
                            
                            if (totalCost === 0) {
                                return '';
                            }
                            
                            return `Total: $${totalCost.toFixed(3)}`;
                        }
                    }
                }
            },
            scales: {
                x: {
                    ticks: {
                        color: '#e7e9ea',
                        font: {
                            size: 11
                        },
                        maxRotation: chartType === 'bar' ? 45 : 45,
                        minRotation: 0
                    },
                    grid: {
                        color: '#2f3336',
                        display: false // No grid lines for bar charts
                    },
                    border: {
                        color: '#e7e9ea'
                    }
                },
                y: {
                    beginAtZero: true,
                    ticks: {
                        color: '#e7e9ea',
                        font: {
                            size: 12
                        },
                        callback: function(value) {
                            return '$' + value.toFixed(3);
                        }
                    },
                    grid: {
                        color: '#2f3336'
                    },
                    border: {
                        color: '#e7e9ea'
                    }
                }
            }
        }
    });
}

function filterCreditsTable(type) {
    const filterSelect = document.getElementById(`${type}Filter`);
    const tableBody = document.getElementById(`${type}TableBody`);
    if (!filterSelect || !tableBody) return;
    
    const filterValue = filterSelect.value;
    const rows = tableBody.querySelectorAll('tr.usage-table-row');
    
    rows.forEach(row => {
        if (filterValue === 'all') {
            row.style.display = '';
        } else {
            const rowType = row.getAttribute('data-type');
            row.style.display = rowType === filterValue ? '' : 'none';
        }
    });
}

// Add hover effects via JavaScript since inline styles don't support :hover
document.addEventListener('DOMContentLoaded', function() {
    // This will be called when the page loads, but we need to attach listeners after table is rendered
    setTimeout(function() {
        const rows = document.querySelectorAll('.usage-table-row');
        rows.forEach(row => {
            row.addEventListener('mouseenter', function() {
                this.style.background = '#16181c';
                const badge = this.querySelector('.usage-type-badge');
                const cost = this.querySelector('.usage-cost');
                if (badge) {
                    badge.style.background = 'rgba(120, 86, 255, 0.25)';
                    badge.style.borderColor = 'rgba(120, 86, 255, 0.5)';
                }
                if (cost) {
                    cost.style.background = 'rgba(120, 86, 255, 0.3)';
                    cost.style.borderColor = 'rgba(120, 86, 255, 0.6)';
                    cost.style.boxShadow = '0 2px 8px rgba(120, 86, 255, 0.2)';
                }
            });
            row.addEventListener('mouseleave', function() {
                this.style.background = '#000000';
                const badge = this.querySelector('.usage-type-badge');
                const cost = this.querySelector('.usage-cost');
                if (badge) {
                    badge.style.background = 'rgba(120, 86, 255, 0.15)';
                    badge.style.borderColor = 'rgba(120, 86, 255, 0.3)';
                }
                if (cost) {
                    cost.style.background = 'rgba(120, 86, 255, 0.2)';
                    cost.style.borderColor = 'rgba(120, 86, 255, 0.4)';
                    cost.style.boxShadow = '0 2px 4px rgba(120, 86, 255, 0.1)';
                }
            });
        });
    }, 500);
});

async function refreshCredits() {
    await loadCredits();
        showToast('Usage data refreshed', 'success');
}

