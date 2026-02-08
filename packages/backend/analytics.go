package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gameHistoryEntry tracks a game's cumulative hours and sessions at a point in time.
type gameHistoryEntry struct {
	Timestamp int64
	Hours     float64
	Sessions  int
}

// snapshotCache caches loaded snapshots in memory with a TTL.
type snapshotCache struct {
	mu        sync.RWMutex
	snapshots []Snapshot
	loadedAt  time.Time
	ttl       time.Duration
}

var cache = &snapshotCache{ttl: 5 * time.Minute}

func (c *snapshotCache) get() ([]Snapshot, error) {
	c.mu.RLock()
	if c.snapshots != nil && time.Since(c.loadedAt) < c.ttl {
		s := c.snapshots
		c.mu.RUnlock()
		return s, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	// Double-check after acquiring write lock
	if c.snapshots != nil && time.Since(c.loadedAt) < c.ttl {
		return c.snapshots, nil
	}

	snapshots, err := loadAllSnapshots()
	if err != nil {
		return nil, err
	}
	c.snapshots = snapshots
	c.loadedAt = time.Now()
	return snapshots, nil
}

func (c *snapshotCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.snapshots = nil
}

type GameTitle struct {
	TitleID      string `json:"titleId"`
	Name         string `json:"name"`
	PlayCount    int    `json:"playCount"`
	PlayDuration string `json:"playDuration"`
	Category     string `json:"category"`
}

type Snapshot struct {
	Titles    []GameTitle        `json:"titles"`
	Timestamp int64
	Filename  string
	GenreMap  map[string][]string // titleID -> genres
}

type MonthlyStats struct {
	Month         string  `json:"month"`
	HoursPlayed   float64 `json:"hoursPlayed"`
	SessionsPlayed int     `json:"sessionsPlayed"`
	GamesPlayed   int     `json:"gamesPlayed"`
	NewGames      int     `json:"newGames"`
}

type GameProgress struct {
	Name          string  `json:"name"`
	TotalHours    float64 `json:"totalHours"`
	TotalSessions int     `json:"totalSessions"`
	FirstSeen     string  `json:"firstSeen"`
	LastPlayed    string  `json:"lastPlayed"`
	MonthlyData   []MonthlyGameData `json:"monthlyData"`
}

type MonthlyGameData struct {
	Month         string  `json:"month"`
	Hours         float64 `json:"hours"`
	Sessions      int     `json:"sessions"`
}

type Analytics struct {
	MonthlyActivity []MonthlyStats          `json:"monthlyActivity"`
	TopGames        []GameProgress          `json:"topGames"`
	TotalStats      TotalStats              `json:"totalStats"`
	Streaks         Streaks                 `json:"streaks"`
}

type TotalStats struct {
	TotalGames      int     `json:"totalGames"`
	TotalHours      float64 `json:"totalHours"`
	TotalSessions   int     `json:"totalSessions"`
	AvgSessionMins  float64 `json:"avgSessionMins"`
	DaysTracked     int     `json:"daysTracked"`
}

type Streaks struct {
	LongestStreak int      `json:"longestStreak"`
	CurrentStreak int      `json:"currentStreak"`
	MostPlayed30Days string `json:"mostPlayed30Days"`
	MostPlayed7Days  string `json:"mostPlayed7Days"`
}

func parseDuration(duration string) float64 {
	re := regexp.MustCompile(`PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)
	matches := re.FindStringSubmatch(duration)
	if matches == nil {
		return 0
	}

	hours := 0.0
	if matches[1] != "" {
		h, _ := strconv.Atoi(matches[1])
		hours = float64(h)
	}
	if matches[2] != "" {
		m, _ := strconv.Atoi(matches[2])
		hours += float64(m) / 60.0
	}
	if matches[3] != "" {
		s, _ := strconv.Atoi(matches[3])
		hours += float64(s) / 3600.0
	}

	return hours
}

func loadAllSnapshots() ([]Snapshot, error) {
	files, err := os.ReadDir(outputDir)
	if err != nil {
		return nil, err
	}

	var snapshots []Snapshot

	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "output_") || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		// Extract timestamp from filename
		timestampStr := file.Name()[7 : len(file.Name())-5]
		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			continue
		}

		content, err := os.ReadFile(filepath.Join(outputDir, file.Name()))
		if err != nil {
			continue
		}

		var data struct {
			Titles []GameTitle `json:"titles"`
		}
		if err := json.Unmarshal(content, &data); err != nil {
			continue
		}

		// Parse genre data from concept.genres per title
		var rawData struct {
			Titles []struct {
				TitleID string `json:"titleId"`
				Concept struct {
					Genres []string `json:"genres"`
				} `json:"concept"`
			} `json:"titles"`
		}
		genreMap := make(map[string][]string)
		if err := json.Unmarshal(content, &rawData); err == nil {
			for _, t := range rawData.Titles {
				if len(t.Concept.Genres) > 0 {
					genreMap[t.TitleID] = t.Concept.Genres
				}
			}
		}

		snapshots = append(snapshots, Snapshot{
			Titles:    data.Titles,
			Timestamp: timestamp,
			Filename:  file.Name(),
			GenreMap:  genreMap,
		})
	}

	// Sort by timestamp
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp < snapshots[j].Timestamp
	})

	return snapshots, nil
}

// buildGameHistory builds a map of titleID -> chronological history entries from snapshots.
func buildGameHistory(snapshots []Snapshot) map[string][]gameHistoryEntry {
	history := make(map[string][]gameHistoryEntry)
	for _, snapshot := range snapshots {
		for _, game := range snapshot.Titles {
			if !strings.Contains(game.Category, "game") {
				continue
			}
			history[game.TitleID] = append(history[game.TitleID], gameHistoryEntry{
				Timestamp: snapshot.Timestamp,
				Hours:     parseDuration(game.PlayDuration),
				Sessions:  game.PlayCount,
			})
		}
	}
	return history
}

func calculateAnalytics(snapshots []Snapshot) *Analytics {
	if len(snapshots) == 0 {
		return &Analytics{}
	}

	gameHistory := buildGameHistory(snapshots)

	// Calculate monthly activity (actual changes)
	monthlyStats := calculateMonthlyActivity(snapshots, gameHistory)

	// Calculate top games with progress
	topGames := calculateTopGames(snapshots[len(snapshots)-1], gameHistory)

	// Calculate total stats
	latest := snapshots[len(snapshots)-1]
	totalHours := 0.0
	totalSessions := 0
	gameCount := 0

	for _, game := range latest.Titles {
		if strings.Contains(game.Category, "game") {
			totalHours += parseDuration(game.PlayDuration)
			totalSessions += game.PlayCount
			gameCount++
		}
	}

	avgSessionMins := 0.0
	if totalSessions > 0 {
		avgSessionMins = (totalHours * 60) / float64(totalSessions)
	}

	daysTracked := int((snapshots[len(snapshots)-1].Timestamp - snapshots[0].Timestamp) / 86400)

	// Calculate streaks
	streaks := calculateStreaks(snapshots, gameHistory)

	return &Analytics{
		MonthlyActivity: monthlyStats,
		TopGames:        topGames,
		TotalStats: TotalStats{
			TotalGames:     gameCount,
			TotalHours:     totalHours,
			TotalSessions:  totalSessions,
			AvgSessionMins: avgSessionMins,
			DaysTracked:    daysTracked,
		},
		Streaks: streaks,
	}
}

func calculateMonthlyActivity(snapshots []Snapshot, gameHistory map[string][]gameHistoryEntry) []MonthlyStats {
	monthlyMap := make(map[string]*MonthlyStats)
	seenGames := make(map[string]map[string]bool) // month -> gameID -> seen

	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]

		currTime := time.Unix(curr.Timestamp, 0)
		prevTime := time.Unix(prev.Timestamp, 0)
		monthKey := currTime.Format("2006-01")
		prevMonthKey := prevTime.Format("2006-01")

		// Skip if previous and current are in different months
		// We only want to count hours gained WITHIN each month
		if prevMonthKey != monthKey {
			continue
		}

		if monthlyMap[monthKey] == nil {
			monthlyMap[monthKey] = &MonthlyStats{
				Month: monthKey,
			}
			seenGames[monthKey] = make(map[string]bool)
		}

		// Build maps for quick lookup
		prevGames := make(map[string]GameTitle)
		for _, g := range prev.Titles {
			if strings.Contains(g.Category, "game") {
				prevGames[g.TitleID] = g
			}
		}

		currGames := make(map[string]GameTitle)
		for _, g := range curr.Titles {
			if strings.Contains(g.Category, "game") {
				currGames[g.TitleID] = g
			}
		}

		// Calculate changes
		for titleID, currGame := range currGames {
			prevGame, existed := prevGames[titleID]

			// Only count hours gained if we have a previous snapshot
			// If it's the first time we see this game, we can't know how much was played before
			if existed {
				currHours := parseDuration(currGame.PlayDuration)
				currSessions := currGame.PlayCount
				prevHours := parseDuration(prevGame.PlayDuration)
				prevSessions := prevGame.PlayCount

				hoursGained := currHours - prevHours
				sessionsGained := currSessions - prevSessions

				// Filter out anomalous jumps (>1000h) likely from save sync issues
				// These are contained/ignored to prevent data pollution
				if hoursGained > 1000 {
					// Skip anomalous data
					continue
				}

				if hoursGained > 0 || sessionsGained > 0 {
					monthlyMap[monthKey].HoursPlayed += hoursGained
					monthlyMap[monthKey].SessionsPlayed += sessionsGained

					if !seenGames[monthKey][titleID] {
						monthlyMap[monthKey].GamesPlayed++
						seenGames[monthKey][titleID] = true
					}
				}
			}
			// If !existed, skip it - we don't know when those hours were accumulated
		}
	}

	// Convert map to sorted slice
	var result []MonthlyStats
	for _, stats := range monthlyMap {
		result = append(result, *stats)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Month < result[j].Month
	})

	return result
}

func calculateTopGames(latest Snapshot, gameHistory map[string][]gameHistoryEntry) []GameProgress {
	var games []GameProgress

	for _, game := range latest.Titles {
		if !strings.Contains(game.Category, "game") {
			continue
		}

		history, exists := gameHistory[game.TitleID]
		if !exists || len(history) == 0 {
			continue
		}

		// Calculate monthly progress
		monthlyData := make(map[string]*MonthlyGameData)

		for i := 1; i < len(history); i++ {
			prev := history[i-1]
			curr := history[i]

			currTime := time.Unix(curr.Timestamp, 0)
			monthKey := currTime.Format("2006-01")

			if monthlyData[monthKey] == nil {
				monthlyData[monthKey] = &MonthlyGameData{
					Month: monthKey,
				}
			}

			hoursGained := curr.Hours - prev.Hours
			sessionsGained := curr.Sessions - prev.Sessions

			if hoursGained > 0 || sessionsGained > 0 {
				monthlyData[monthKey].Hours += hoursGained
				monthlyData[monthKey].Sessions += sessionsGained
			}
		}

		var monthly []MonthlyGameData
		for _, data := range monthlyData {
			monthly = append(monthly, *data)
		}

		sort.Slice(monthly, func(i, j int) bool {
			return monthly[i].Month < monthly[j].Month
		})

		firstSeen := time.Unix(history[0].Timestamp, 0).Format("2006-01-02")
		lastPlayed := time.Unix(history[len(history)-1].Timestamp, 0).Format("2006-01-02")

		games = append(games, GameProgress{
			Name:          game.Name,
			TotalHours:    parseDuration(game.PlayDuration),
			TotalSessions: game.PlayCount,
			FirstSeen:     firstSeen,
			LastPlayed:    lastPlayed,
			MonthlyData:   monthly,
		})
	}

	// Sort by total hours
	sort.Slice(games, func(i, j int) bool {
		return games[i].TotalHours > games[j].TotalHours
	})

	// Return top 10
	if len(games) > 10 {
		games = games[:10]
	}

	return games
}

func calculateStreaks(snapshots []Snapshot, gameHistory map[string][]gameHistoryEntry) Streaks {
	if len(snapshots) < 2 {
		return Streaks{}
	}

	// Calculate gaming streak (consecutive days with playtime increase)
	longestStreak := 0
	currentStreak := 0
	lastWasActive := false

	for i := 1; i < len(snapshots); i++ {
		wasActive := false

		// Check if ANY game had progress
		prevGames := make(map[string]GameTitle)
		for _, g := range snapshots[i-1].Titles {
			prevGames[g.TitleID] = g
		}

		for _, currGame := range snapshots[i].Titles {
			if !strings.Contains(currGame.Category, "game") {
				continue
			}

			prevGame, existed := prevGames[currGame.TitleID]
			if existed {
				currHours := parseDuration(currGame.PlayDuration)
				prevHours := parseDuration(prevGame.PlayDuration)
				if currHours > prevHours {
					wasActive = true
					break
				}
			}
		}

		if wasActive {
			if lastWasActive {
				currentStreak++
			} else {
				currentStreak = 1
			}
			if currentStreak > longestStreak {
				longestStreak = currentStreak
			}
		} else {
			currentStreak = 0
		}

		lastWasActive = wasActive
	}

	// Most played in last 30 and 7 days
	mostPlayed30 := findMostPlayedRecent(snapshots, gameHistory, 30)
	mostPlayed7 := findMostPlayedRecent(snapshots, gameHistory, 7)

	return Streaks{
		LongestStreak:    longestStreak,
		CurrentStreak:    currentStreak,
		MostPlayed30Days: mostPlayed30,
		MostPlayed7Days:  mostPlayed7,
	}
}

func findMostPlayedRecent(snapshots []Snapshot, gameHistory map[string][]gameHistoryEntry, days int) string {
	if len(snapshots) == 0 {
		return "N/A"
	}

	latest := snapshots[len(snapshots)-1]
	cutoff := latest.Timestamp - int64(days*86400)

	gameGains := make(map[string]float64)
	gameNames := make(map[string]string)

	for _, game := range latest.Titles {
		if !strings.Contains(game.Category, "game") {
			continue
		}

		history, exists := gameHistory[game.TitleID]
		if !exists {
			continue
		}

		// Find snapshot closest to cutoff
		var startHours float64
		for _, h := range history {
			if h.Timestamp <= cutoff {
				startHours = h.Hours
			} else {
				break
			}
		}

		currentHours := parseDuration(game.PlayDuration)
		gain := currentHours - startHours

		if gain > 0 {
			gameGains[game.TitleID] = gain
			gameNames[game.TitleID] = game.Name
		}
	}

	// Find max
	maxGain := 0.0
	maxGame := "N/A"
	for id, gain := range gameGains {
		if gain > maxGain {
			maxGain = gain
			maxGame = gameNames[id]
		}
	}

	return maxGame
}

func getAnalytics() (*Analytics, error) {
	log.Println("Loading snapshots for analytics...")
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	log.Printf("Loaded %d snapshots", len(snapshots))

	if len(snapshots) == 0 {
		return &Analytics{}, nil
	}

	analytics := calculateAnalytics(snapshots)
	log.Println("Analytics calculated successfully")

	return analytics, nil
}

// MonthlyGamesPlayed represents games played in a specific month
type MonthlyGamesPlayed struct {
	Month      string               `json:"month"`
	Games      []MonthlyGameSummary `json:"games"`
	TotalHours float64              `json:"totalHours"`
}

type MonthlyGameSummary struct {
	Name         string  `json:"name"`
	HoursGained  float64 `json:"hoursGained"`
	SessionsGained int   `json:"sessionsGained"`
	TitleID      string  `json:"titleId"`
}

// YearlyTopGames represents top games for a specific year
type YearlyTopGames struct {
	Year  int                  `json:"year"`
	Games []YearlyGameSummary  `json:"games"`
}

type YearlyGameSummary struct {
	Name        string  `json:"name"`
	HoursGained float64 `json:"hoursGained"`
	TitleID     string  `json:"titleId"`
}

// Milestones represents gaming achievements
type Milestones struct {
	Games50Hours   []string `json:"games50Hours"`
	Games100Hours  []string `json:"games100Hours"`
	Games200Hours  []string `json:"games200Hours"`
	LongestSession string   `json:"longestSession"`
	TotalDays      int      `json:"totalDays"`
}

// getMonthlyGames returns games played in a specific month (YYYY-MM format)
func getMonthlyGames(month string) (*MonthlyGamesPlayed, error) {
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	if len(snapshots) < 2 {
		return &MonthlyGamesPlayed{Month: month, Games: []MonthlyGameSummary{}}, nil
	}

	gameGains := make(map[string]*MonthlyGameSummary)

	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]

		currTime := time.Unix(curr.Timestamp, 0)
		prevTime := time.Unix(prev.Timestamp, 0)
		monthKey := currTime.Format("2006-01")
		prevMonthKey := prevTime.Format("2006-01")

		// Skip if current snapshot is not in the target month
		if monthKey != month {
			continue
		}

		// Skip if previous snapshot is not in the same month
		// We only want to count hours gained WITHIN this month
		if prevMonthKey != monthKey {
			continue
		}

		prevGames := make(map[string]GameTitle)
		for _, g := range prev.Titles {
			if strings.Contains(g.Category, "game") {
				prevGames[g.TitleID] = g
			}
		}

		for _, currGame := range curr.Titles {
			if !strings.Contains(currGame.Category, "game") {
				continue
			}

			prevGame, existed := prevGames[currGame.TitleID]

			// Only count hours gained if we have a previous snapshot
			// If it's the first time we see this game, we can't know how much was played before
			if existed {
				currHours := parseDuration(currGame.PlayDuration)
				currSessions := currGame.PlayCount
				prevHours := parseDuration(prevGame.PlayDuration)
				prevSessions := prevGame.PlayCount

				hoursGained := currHours - prevHours
				sessionsGained := currSessions - prevSessions

				// Filter out anomalous jumps (>1000h) likely from save sync issues
				// These are contained/ignored to prevent data pollution
				if hoursGained > 1000 {
					// Skip anomalous data
					continue
				}

				if hoursGained > 0 || sessionsGained > 0 {
					if gameGains[currGame.TitleID] == nil {
						gameGains[currGame.TitleID] = &MonthlyGameSummary{
							Name:    currGame.Name,
							TitleID: currGame.TitleID,
						}
					}
					gameGains[currGame.TitleID].HoursGained += hoursGained
					gameGains[currGame.TitleID].SessionsGained += sessionsGained
				}
			}
			// If !existed, we skip it - we don't know when those hours were accumulated
		}
	}

	var games []MonthlyGameSummary
	totalHours := 0.0
	for _, game := range gameGains {
		games = append(games, *game)
		totalHours += game.HoursGained
	}

	sort.Slice(games, func(i, j int) bool {
		return games[i].HoursGained > games[j].HoursGained
	})

	return &MonthlyGamesPlayed{
		Month:      month,
		Games:      games,
		TotalHours: totalHours,
	}, nil
}

// getYearlyTopGames returns top games for a specific year
func getYearlyTopGames(year int) (*YearlyTopGames, error) {
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	if len(snapshots) < 2 {
		return &YearlyTopGames{Year: year, Games: []YearlyGameSummary{}}, nil
	}

	gameGains := make(map[string]*YearlyGameSummary)

	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]

		currTime := time.Unix(curr.Timestamp, 0)
		prevTime := time.Unix(prev.Timestamp, 0)

		// Skip if current snapshot is not in the target year
		if currTime.Year() != year {
			continue
		}

		// Skip if previous snapshot is not in the same year
		// We only want to count hours gained WITHIN this year
		if prevTime.Year() != currTime.Year() {
			continue
		}

		prevGames := make(map[string]GameTitle)
		for _, g := range prev.Titles {
			if strings.Contains(g.Category, "game") {
				prevGames[g.TitleID] = g
			}
		}

		for _, currGame := range curr.Titles {
			if !strings.Contains(currGame.Category, "game") {
				continue
			}

			prevGame, existed := prevGames[currGame.TitleID]

			// Only count hours gained if we have a previous snapshot
			// If it's the first time we see this game, we can't know how much was played before
			if existed {
				currHours := parseDuration(currGame.PlayDuration)
				prevHours := parseDuration(prevGame.PlayDuration)
				hoursGained := currHours - prevHours

				// Filter out anomalous jumps (>1000h) likely from save sync issues
				// These are contained/ignored to prevent data pollution
				if hoursGained > 1000 {
					// Skip anomalous data
					continue
				}

				if hoursGained > 0 {
					if gameGains[currGame.TitleID] == nil {
						gameGains[currGame.TitleID] = &YearlyGameSummary{
							Name:    currGame.Name,
							TitleID: currGame.TitleID,
						}
					}
					gameGains[currGame.TitleID].HoursGained += hoursGained
				}
			}
			// If !existed, we skip it - we don't know when those hours were accumulated
		}
	}

	var games []YearlyGameSummary
	for _, game := range gameGains {
		games = append(games, *game)
	}

	sort.Slice(games, func(i, j int) bool {
		return games[i].HoursGained > games[j].HoursGained
	})

	// Return top 25
	if len(games) > 25 {
		games = games[:25]
	}

	return &YearlyTopGames{
		Year:  year,
		Games: games,
	}, nil
}

// getMilestones returns gaming milestones
func getMilestones() (*Milestones, error) {
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	if len(snapshots) == 0 {
		return &Milestones{}, nil
	}

	latest := snapshots[len(snapshots)-1]
	var games50, games100, games200 []string

	for _, game := range latest.Titles {
		if !strings.Contains(game.Category, "game") {
			continue
		}

		hours := parseDuration(game.PlayDuration)
		if hours >= 200 {
			games200 = append(games200, game.Name)
		} else if hours >= 100 {
			games100 = append(games100, game.Name)
		} else if hours >= 50 {
			games50 = append(games50, game.Name)
		}
	}

	// Calculate total days tracked
	totalDays := 0
	if len(snapshots) > 1 {
		totalDays = int((snapshots[len(snapshots)-1].Timestamp - snapshots[0].Timestamp) / 86400)
	}

	return &Milestones{
		Games50Hours:  games50,
		Games100Hours: games100,
		Games200Hours: games200,
		TotalDays:     totalDays,
	}, nil
}

// getAvailableYears returns all years that have data
func getAvailableYears() ([]int, error) {
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	if len(snapshots) == 0 {
		return []int{}, nil
	}

	yearsMap := make(map[int]bool)
	for _, snapshot := range snapshots {
		year := time.Unix(snapshot.Timestamp, 0).Year()
		yearsMap[year] = true
	}

	var years []int
	for year := range yearsMap {
		years = append(years, year)
	}

	sort.Ints(years)

	// Reverse to show newest first
	for i, j := 0, len(years)-1; i < j; i, j = i+1, j-1 {
		years[i], years[j] = years[j], years[i]
	}

	return years, nil
}

// ─── Year-over-Year Comparison ───

type YearlySummary struct {
	Year           int     `json:"year"`
	TotalHours     float64 `json:"totalHours"`
	TotalSessions  int     `json:"totalSessions"`
	GamesPlayed    int     `json:"gamesPlayed"`
}

type YoYMonthly struct {
	Month string             `json:"month"` // "Jan", "Feb", etc.
	Years map[string]float64 `json:"years"` // year string -> hours
}

type YoYComparison struct {
	Summaries []YearlySummary `json:"summaries"`
	Monthly   []YoYMonthly    `json:"monthly"`
}

func getYoYComparison() (*YoYComparison, error) {
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	if len(snapshots) < 2 {
		return &YoYComparison{}, nil
	}

	// Build full uncapped monthly activity
	gameHistory := buildGameHistory(snapshots)
	monthlyStats := calculateMonthlyActivity(snapshots, gameHistory)

	// Pivot monthly data into year -> month structure
	yearMonthHours := make(map[int]map[int]float64)   // year -> monthNum -> hours
	yearMonthSessions := make(map[int]map[int]int)     // year -> monthNum -> sessions
	yearGames := make(map[int]map[string]bool)         // year -> gameIDs

	for _, ms := range monthlyStats {
		// Parse "2024-03" into year and month
		parts := strings.Split(ms.Month, "-")
		if len(parts) != 2 {
			continue
		}
		year, _ := strconv.Atoi(parts[0])
		month, _ := strconv.Atoi(parts[1])

		if yearMonthHours[year] == nil {
			yearMonthHours[year] = make(map[int]float64)
			yearMonthSessions[year] = make(map[int]int)
			yearGames[year] = make(map[string]bool)
		}
		yearMonthHours[year][month] += ms.HoursPlayed
		yearMonthSessions[year][month] += ms.SessionsPlayed
	}

	// Count games per year from snapshot data
	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]
		currTime := time.Unix(curr.Timestamp, 0)
		prevTime := time.Unix(prev.Timestamp, 0)
		if prevTime.Year() != currTime.Year() {
			continue
		}
		year := currTime.Year()
		if yearGames[year] == nil {
			yearGames[year] = make(map[string]bool)
		}

		prevMap := make(map[string]GameTitle)
		for _, g := range prev.Titles {
			if strings.Contains(g.Category, "game") {
				prevMap[g.TitleID] = g
			}
		}
		for _, g := range curr.Titles {
			if !strings.Contains(g.Category, "game") {
				continue
			}
			if pg, ok := prevMap[g.TitleID]; ok {
				if parseDuration(g.PlayDuration) > parseDuration(pg.PlayDuration) {
					yearGames[year][g.TitleID] = true
				}
			}
		}
	}

	// Build summaries
	var years []int
	for y := range yearMonthHours {
		years = append(years, y)
	}
	sort.Ints(years)

	var summaries []YearlySummary
	for _, y := range years {
		totalH := 0.0
		totalS := 0
		for _, h := range yearMonthHours[y] {
			totalH += h
		}
		for _, s := range yearMonthSessions[y] {
			totalS += s
		}
		summaries = append(summaries, YearlySummary{
			Year:          y,
			TotalHours:    totalH,
			TotalSessions: totalS,
			GamesPlayed:   len(yearGames[y]),
		})
	}

	// Build monthly overlay (Jan=1 .. Dec=12)
	monthNames := []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	var monthly []YoYMonthly
	for m := 1; m <= 12; m++ {
		entry := YoYMonthly{
			Month: monthNames[m],
			Years: make(map[string]float64),
		}
		for _, y := range years {
			if h, ok := yearMonthHours[y][m]; ok {
				entry.Years[strconv.Itoa(y)] = h
			}
		}
		monthly = append(monthly, entry)
	}

	return &YoYComparison{
		Summaries: summaries,
		Monthly:   monthly,
	}, nil
}

// ─── Per-Game Deep Dive ───

type CumulativePoint struct {
	Month          string  `json:"month"`
	CumulativeHours float64 `json:"cumulativeHours"`
}

type GameDeepDive struct {
	TitleID        string            `json:"titleId"`
	Name           string            `json:"name"`
	TotalHours     float64           `json:"totalHours"`
	TotalSessions  int               `json:"totalSessions"`
	AvgSessionMins float64           `json:"avgSessionMins"`
	FirstSeen      string            `json:"firstSeen"`
	LastPlayed     string            `json:"lastPlayed"`
	PeakMonth      string            `json:"peakMonth"`
	PeakMonthHours float64           `json:"peakMonthHours"`
	MonthlyData    []MonthlyGameData `json:"monthlyData"`
	Cumulative     []CumulativePoint `json:"cumulative"`
	Genres         []string          `json:"genres"`
}

func getGameDeepDive(titleID string) (*GameDeepDive, error) {
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	if len(snapshots) < 2 {
		return nil, nil
	}

	gameHistory := buildGameHistory(snapshots)
	history, exists := gameHistory[titleID]
	if !exists || len(history) == 0 {
		return nil, nil
	}

	// Find the game name and genres from the latest snapshot
	var gameName string
	var genres []string
	latest := snapshots[len(snapshots)-1]
	for _, g := range latest.Titles {
		if g.TitleID == titleID {
			gameName = g.Name
			break
		}
	}
	if latest.GenreMap != nil {
		genres = latest.GenreMap[titleID]
	}

	// If not in latest snapshot, search backwards
	if gameName == "" {
		for i := len(snapshots) - 2; i >= 0; i-- {
			for _, g := range snapshots[i].Titles {
				if g.TitleID == titleID {
					gameName = g.Name
					break
				}
			}
			if gameName != "" {
				break
			}
		}
	}

	// Calculate monthly breakdown
	monthlyData := make(map[string]*MonthlyGameData)
	for i := 1; i < len(history); i++ {
		prev := history[i-1]
		curr := history[i]

		currTime := time.Unix(curr.Timestamp, 0)
		monthKey := currTime.Format("2006-01")

		if monthlyData[monthKey] == nil {
			monthlyData[monthKey] = &MonthlyGameData{Month: monthKey}
		}

		hoursGained := curr.Hours - prev.Hours
		sessionsGained := curr.Sessions - prev.Sessions

		if hoursGained > 0 && hoursGained < 1000 {
			monthlyData[monthKey].Hours += hoursGained
		}
		if sessionsGained > 0 {
			monthlyData[monthKey].Sessions += sessionsGained
		}
	}

	var monthly []MonthlyGameData
	for _, data := range monthlyData {
		monthly = append(monthly, *data)
	}
	sort.Slice(monthly, func(i, j int) bool {
		return monthly[i].Month < monthly[j].Month
	})

	// Find peak month
	peakMonth := ""
	peakHours := 0.0
	for _, m := range monthly {
		if m.Hours > peakHours {
			peakHours = m.Hours
			peakMonth = m.Month
		}
	}

	// Build cumulative curve
	var cumulative []CumulativePoint
	cumHours := 0.0
	for _, m := range monthly {
		cumHours += m.Hours
		cumulative = append(cumulative, CumulativePoint{
			Month:           m.Month,
			CumulativeHours: cumHours,
		})
	}

	// Total stats from last entry
	totalHours := history[len(history)-1].Hours
	totalSessions := history[len(history)-1].Sessions
	avgSessionMins := 0.0
	if totalSessions > 0 {
		avgSessionMins = (totalHours * 60) / float64(totalSessions)
	}

	firstSeen := time.Unix(history[0].Timestamp, 0).Format("2006-01-02")
	lastPlayed := time.Unix(history[len(history)-1].Timestamp, 0).Format("2006-01-02")

	return &GameDeepDive{
		TitleID:        titleID,
		Name:           gameName,
		TotalHours:     totalHours,
		TotalSessions:  totalSessions,
		AvgSessionMins: avgSessionMins,
		FirstSeen:      firstSeen,
		LastPlayed:     lastPlayed,
		PeakMonth:      peakMonth,
		PeakMonthHours: peakHours,
		MonthlyData:    monthly,
		Cumulative:     cumulative,
		Genres:         genres,
	}, nil
}

// ─── Genre & Habit Trends ───

type MonthGenreBreakdown struct {
	Month  string             `json:"month"`
	Genres map[string]float64 `json:"genres"` // genre -> hours
}

type SessionTrend struct {
	Month          string  `json:"month"`
	AvgSessionMins float64 `json:"avgSessionMins"`
	TotalSessions  int     `json:"totalSessions"`
}

type GenreShift struct {
	Month         string `json:"month"`
	DominantGenre string `json:"dominantGenre"`
}

type GenreTrends struct {
	MonthlyGenres  []MonthGenreBreakdown `json:"monthlyGenres"`
	SessionTrends  []SessionTrend        `json:"sessionTrends"`
	GenreShifts    []GenreShift          `json:"genreShifts"`
	TopGenres      []string              `json:"topGenres"`
}

func getGenreTrends() (*GenreTrends, error) {
	snapshots, err := cache.get()
	if err != nil {
		return nil, err
	}

	if len(snapshots) < 2 {
		return &GenreTrends{}, nil
	}

	// Build the latest genre map (genres don't change, use latest snapshot)
	latestGenres := snapshots[len(snapshots)-1].GenreMap
	if latestGenres == nil {
		latestGenres = make(map[string][]string)
	}
	// Fill from earlier snapshots for games that might have disappeared
	for i := len(snapshots) - 2; i >= 0; i-- {
		if snapshots[i].GenreMap == nil {
			continue
		}
		for tid, genres := range snapshots[i].GenreMap {
			if _, ok := latestGenres[tid]; !ok {
				latestGenres[tid] = genres
			}
		}
	}

	// Monthly genre hours and session tracking
	monthGenreHours := make(map[string]map[string]float64) // month -> genre -> hours
	monthSessions := make(map[string]int)                   // month -> total sessions
	monthHours := make(map[string]float64)                  // month -> total hours

	for i := 1; i < len(snapshots); i++ {
		prev := snapshots[i-1]
		curr := snapshots[i]

		currTime := time.Unix(curr.Timestamp, 0)
		prevTime := time.Unix(prev.Timestamp, 0)
		monthKey := currTime.Format("2006-01")
		prevMonthKey := prevTime.Format("2006-01")

		if prevMonthKey != monthKey {
			continue
		}

		prevGames := make(map[string]GameTitle)
		for _, g := range prev.Titles {
			if strings.Contains(g.Category, "game") {
				prevGames[g.TitleID] = g
			}
		}

		for _, currGame := range curr.Titles {
			if !strings.Contains(currGame.Category, "game") {
				continue
			}

			prevGame, existed := prevGames[currGame.TitleID]
			if !existed {
				continue
			}

			hoursGained := parseDuration(currGame.PlayDuration) - parseDuration(prevGame.PlayDuration)
			sessionsGained := currGame.PlayCount - prevGame.PlayCount

			if hoursGained > 1000 || hoursGained <= 0 {
				continue
			}

			monthHours[monthKey] += hoursGained
			monthSessions[monthKey] += sessionsGained

			// Attribute hours to genres
			genres := latestGenres[currGame.TitleID]
			if len(genres) == 0 {
				continue
			}
			if monthGenreHours[monthKey] == nil {
				monthGenreHours[monthKey] = make(map[string]float64)
			}
			perGenre := hoursGained / float64(len(genres))
			for _, genre := range genres {
				monthGenreHours[monthKey][genre] += perGenre
			}
		}
	}

	// Collect all months sorted
	var months []string
	for m := range monthGenreHours {
		months = append(months, m)
	}
	sort.Strings(months)

	// Find top 5 genres by total hours across all months
	genreTotalHours := make(map[string]float64)
	for _, genreHours := range monthGenreHours {
		for genre, hours := range genreHours {
			genreTotalHours[genre] += hours
		}
	}

	type genreTotal struct {
		genre string
		hours float64
	}
	var genreTotals []genreTotal
	for g, h := range genreTotalHours {
		genreTotals = append(genreTotals, genreTotal{g, h})
	}
	sort.Slice(genreTotals, func(i, j int) bool {
		return genreTotals[i].hours > genreTotals[j].hours
	})

	topN := 5
	if len(genreTotals) < topN {
		topN = len(genreTotals)
	}
	var topGenres []string
	topGenreSet := make(map[string]bool)
	for i := 0; i < topN; i++ {
		topGenres = append(topGenres, genreTotals[i].genre)
		topGenreSet[genreTotals[i].genre] = true
	}

	// Build monthly breakdowns, session trends, genre shifts
	var monthlyGenres []MonthGenreBreakdown
	var sessionTrends []SessionTrend
	var genreShifts []GenreShift

	for _, month := range months {
		// Monthly genre breakdown (only top genres, rest as "Other")
		genreBreakdown := make(map[string]float64)
		otherHours := 0.0
		for genre, hours := range monthGenreHours[month] {
			if topGenreSet[genre] {
				genreBreakdown[genre] = hours
			} else {
				otherHours += hours
			}
		}
		if otherHours > 0 {
			genreBreakdown["Other"] = otherHours
		}
		monthlyGenres = append(monthlyGenres, MonthGenreBreakdown{
			Month:  month,
			Genres: genreBreakdown,
		})

		// Session trend
		avgMins := 0.0
		if monthSessions[month] > 0 {
			avgMins = (monthHours[month] * 60) / float64(monthSessions[month])
		}
		sessionTrends = append(sessionTrends, SessionTrend{
			Month:          month,
			AvgSessionMins: avgMins,
			TotalSessions:  monthSessions[month],
		})

		// Dominant genre
		maxHours := 0.0
		dominant := ""
		for genre, hours := range monthGenreHours[month] {
			if hours > maxHours {
				maxHours = hours
				dominant = genre
			}
		}
		genreShifts = append(genreShifts, GenreShift{
			Month:         month,
			DominantGenre: dominant,
		})
	}

	return &GenreTrends{
		MonthlyGenres: monthlyGenres,
		SessionTrends: sessionTrends,
		GenreShifts:   genreShifts,
		TopGenres:     topGenres,
	}, nil
}
