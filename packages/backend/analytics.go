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
	"time"
)

type GameTitle struct {
	TitleID      string `json:"titleId"`
	Name         string `json:"name"`
	PlayCount    int    `json:"playCount"`
	PlayDuration string `json:"playDuration"`
	Category     string `json:"category"`
}

type Snapshot struct {
	Titles    []GameTitle `json:"titles"`
	Timestamp int64
	Filename  string
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

		snapshots = append(snapshots, Snapshot{
			Titles:    data.Titles,
			Timestamp: timestamp,
			Filename:  file.Name(),
		})
	}

	// Sort by timestamp
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp < snapshots[j].Timestamp
	})

	return snapshots, nil
}

func calculateAnalytics(snapshots []Snapshot) *Analytics {
	if len(snapshots) == 0 {
		return &Analytics{}
	}

	// Track game progress over time
	gameHistory := make(map[string][]struct {
		Timestamp int64
		Hours     float64
		Sessions  int
	})

	for _, snapshot := range snapshots {
		for _, game := range snapshot.Titles {
			if !strings.Contains(game.Category, "game") {
				continue
			}

			gameHistory[game.TitleID] = append(gameHistory[game.TitleID], struct {
				Timestamp int64
				Hours     float64
				Sessions  int
			}{
				Timestamp: snapshot.Timestamp,
				Hours:     parseDuration(game.PlayDuration),
				Sessions:  game.PlayCount,
			})
		}
	}

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

func calculateMonthlyActivity(snapshots []Snapshot, gameHistory map[string][]struct {
	Timestamp int64
	Hours     float64
	Sessions  int
}) []MonthlyStats {
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

	// Keep last 12 months
	if len(result) > 12 {
		result = result[len(result)-12:]
	}

	return result
}

func calculateTopGames(latest Snapshot, gameHistory map[string][]struct {
	Timestamp int64
	Hours     float64
	Sessions  int
}) []GameProgress {
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

func calculateStreaks(snapshots []Snapshot, gameHistory map[string][]struct {
	Timestamp int64
	Hours     float64
	Sessions  int
}) Streaks {
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

func findMostPlayedRecent(snapshots []Snapshot, gameHistory map[string][]struct {
	Timestamp int64
	Hours     float64
	Sessions  int
}, days int) string {
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
	snapshots, err := loadAllSnapshots()
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
	snapshots, err := loadAllSnapshots()
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
	snapshots, err := loadAllSnapshots()
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

	// Return top 10
	if len(games) > 10 {
		games = games[:10]
	}

	return &YearlyTopGames{
		Year:  year,
		Games: games,
	}, nil
}

// getMilestones returns gaming milestones
func getMilestones() (*Milestones, error) {
	snapshots, err := loadAllSnapshots()
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
	snapshots, err := loadAllSnapshots()
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
