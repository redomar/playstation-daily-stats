import { useState, useEffect, useMemo } from "react";
import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  ComposedChart,
  Tooltip,
  ResponsiveContainer,
  Cell,
  ReferenceLine,
  RadarChart,
  PolarGrid,
  PolarAngleAxis,
  PolarRadiusAxis,
  Radar,
  AreaChart,
} from "recharts";

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Button } from "@/components/ui/button";
import { FilterIcon, FilterXIcon, TrendingUp, Clock, GamepadIcon, BarChart3Icon } from "lucide-react";
import { DiscIcon } from "@radix-ui/react-icons";

interface LocalizedName {
  defaultLanguage: string;
  metadata: {
    [key: string]: string;
  };
}

interface Image {
  format: string;
  type: string;
  url: string;
}

interface Media {
  audios: [];
  images: Image[];
  videos: [];
}

interface Concept {
  id: string;
  name: string;
  localizedName: LocalizedName;
  country: string;
  genres: string[];
  language: string;
  media: Media;
  titleIds: string[];
}

interface Title {
  category: string;
  concept: Concept;
  firstPlayedDateTime: string;
  imageUrl: string;
  lastPlayedDateTime: string;
  localizedImageUrl: string;
  localizedName: string;
  media: Media;
  name: string;
  playCount: number;
  playDuration: string;
  service: string;
  titleId: string;
}

interface Data {
  nextOffset: string;
  previousOffset: string;
  titles: Title[];
  totalItemCount: number;
  timestamp: number;
  filename: string;
}

interface MonthlyStats {
  month: string;
  hoursPlayed: number;
  sessionsPlayed: number;
  gamesPlayed: number;
  newGames: number;
}

interface MonthlyGameData {
  month: string;
  hours: number;
  sessions: number;
}

interface GameProgress {
  name: string;
  totalHours: number;
  totalSessions: number;
  firstSeen: string;
  lastPlayed: string;
  monthlyData: MonthlyGameData[];
}

interface Analytics {
  monthlyActivity: MonthlyStats[];
  topGames: GameProgress[];
  totalStats: {
    totalGames: number;
    totalHours: number;
    totalSessions: number;
    avgSessionMins: number;
    daysTracked: number;
  };
  streaks: {
    longestStreak: number;
    currentStreak: number;
    mostPlayed30Days: string;
    mostPlayed7Days: string;
  };
}

interface Milestones {
  games50Hours: string[];
  games100Hours: string[];
  games200Hours: string[];
  longestSession: string;
  totalDays: number;
}

interface MonthlyGamesPlayed {
  month: string;
  games: {
    name: string;
    hoursGained: number;
    sessionsGained: number;
    titleId: string;
  }[];
  totalHours: number;
}

interface YearlyTopGames {
  year: number;
  games: {
    name: string;
    hoursGained: number;
    titleId: string;
  }[];
}

const serviceMap = new Map([
  ["none(purchased)", "Digital Licence"],
  ["other", "Physical or Other Licence"],
  ["ps_plus", "PlayStation Plus"],
  ["ea_access", "EA Access"],
  ["none_purchased", "Unknown"],
]) as Map<string, string>;

function shortenString(str: string): string {
  if (str.includes("_")) {
    return str
      .split("_")
      .map((word) => word[0])
      .join("");
  }
  return str;
}

const getDurationInSeconds = (duration: string) => {
  const match = duration.match(/PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?/);
  if (!match) return 0;
  const hours = parseInt(match[1] || "0") * 3600;
  const minutes = parseInt(match[2] || "0") * 60;
  const seconds = parseInt(match[3] || "0");
  return hours + minutes + seconds;
};

const getDurationInHours = (duration: string) => {
  return getDurationInSeconds(duration) / 3600;
};

export function GamesListV2() {
  const [data, setData] = useState<Data | null>(null);
  const [analytics, setAnalytics] = useState<Analytics | null>(null);
  const [milestones, setMilestones] = useState<Milestones | null>(null);
  const [monthlyGames, setMonthlyGames] = useState<MonthlyGamesPlayed | null>(null);
  const [yearlyGames, setYearlyGames] = useState<YearlyTopGames | null>(null);
  const [availableYears, setAvailableYears] = useState<number[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [sortBy, setSortBy] = useState<string>("lastPlayed");
  const [filterGenre, setFilterGenre] = useState<string>("all");
  const [filterService, setFilterService] = useState<string>("all");
  const [scaleType, setScaleType] = useState<"log" | "linear">("log");
  const [chartType, setChartType] = useState<"distribution" | "sessions" | "genres" | "timeline" | "topgames">("distribution");
  const [selectedMonth, setSelectedMonth] = useState<string>("");
  const [selectedYear, setSelectedYear] = useState<number>(new Date().getFullYear());

  const origins = useMemo(
    () =>
      import.meta.env.VITE_ALLOWED_ORIGINS?.split(",") ?? [
        import.meta.env.VITE_ALLOWED_ORIGINS,
      ],
    []
  );
  const baseUri = useMemo(() => origins[0], [origins]);

  useEffect(() => {
    const fetchData = async () => {
      try {
        // Fetch latest snapshot
        const response = await fetch(`${baseUri}/api/latest-output`);
        if (!response.ok) {
          throw new Error("Network response was not ok");
        }
        setData(await response.json());

        // Fetch analytics
        const analyticsResponse = await fetch(`${baseUri}/api/analytics`);
        if (analyticsResponse.ok) {
          const analyticsData = await analyticsResponse.json();
          setAnalytics(analyticsData);

          // Set default selected month to the latest month in analytics
          if (analyticsData.monthlyActivity && analyticsData.monthlyActivity.length > 0) {
            const latestMonth = analyticsData.monthlyActivity[analyticsData.monthlyActivity.length - 1].month;
            setSelectedMonth(latestMonth);
          }
        }

        // Fetch milestones
        const milestonesResponse = await fetch(`${baseUri}/api/analytics/milestones`);
        if (milestonesResponse.ok) {
          setMilestones(await milestonesResponse.json());
        }

        // Fetch available years
        const yearsResponse = await fetch(`${baseUri}/api/analytics/years`);
        if (yearsResponse.ok) {
          const years = await yearsResponse.json();
          setAvailableYears(years);
          if (years.length > 0) {
            setSelectedYear(years[0]); // Set to most recent year
          }
        }
      } catch (error) {
        console.error("Error fetching data:", error);
        setError("Failed to fetch data");
      }
    };

    fetchData();
  }, [baseUri]);

  // Fetch monthly games when month changes
  useEffect(() => {
    if (!selectedMonth || !baseUri) return;

    const fetchMonthlyGames = async () => {
      try {
        const response = await fetch(`${baseUri}/api/analytics/monthly/${selectedMonth}`);
        if (response.ok) {
          setMonthlyGames(await response.json());
        }
      } catch (error) {
        console.error("Error fetching monthly games:", error);
      }
    };

    fetchMonthlyGames();
  }, [selectedMonth, baseUri]);

  // Fetch yearly games when year changes
  useEffect(() => {
    if (!selectedYear || !baseUri) return;

    const fetchYearlyGames = async () => {
      try {
        const response = await fetch(`${baseUri}/api/analytics/yearly/${selectedYear}`);
        if (response.ok) {
          setYearlyGames(await response.json());
        }
      } catch (error) {
        console.error("Error fetching yearly games:", error);
      }
    };

    fetchYearlyGames();
  }, [selectedYear, baseUri]);

  const getAllGenres = (titles: Title[]) => {
    const genres = new Set<string>();
    titles.forEach((title) =>
      title.concept.genres.forEach((genre) => genres.add(genre))
    );
    return Array.from(genres);
  };

  const getAllServices = (titles: Title[]) => {
    const services = new Set<string>();
    titles.forEach((title) => services.add(title.service));
    return Array.from(services);
  };

  const sortTitles = (titles: Title[]) => {
    return [...titles].sort((a, b) => {
      switch (sortBy) {
        case "lastPlayed":
          return (
            new Date(b.lastPlayedDateTime).getTime() -
            new Date(a.lastPlayedDateTime).getTime()
          );
        case "mostPlayed":
          return b.playCount - a.playCount;
        case "playTime":
          return (
            getDurationInSeconds(b.playDuration) -
            getDurationInSeconds(a.playDuration)
          );
        case "avgSession":
          const avgA = a.playCount > 0 ? getDurationInSeconds(a.playDuration) / a.playCount : 0;
          const avgB = b.playCount > 0 ? getDurationInSeconds(b.playDuration) / b.playCount : 0;
          return avgB - avgA;
        case "name":
          return a.name.localeCompare(b.name);
        default:
          return 0;
      }
    });
  };

  const filterTitles = (titles: Title[]) => {
    return titles.filter((title) => {
      const genreMatch =
        filterGenre === "all" || title.concept.genres.includes(filterGenre);
      const serviceMatch =
        filterService === "all" || title.service === filterService;
      return genreMatch && serviceMatch;
    });
  };

  const getAggregations = (titles: Title[]) => {
    const totalSeconds = titles.reduce((acc, title) => acc + getDurationInSeconds(title.playDuration), 0);
    const totalPlayCount = titles.reduce((acc, title) => acc + title.playCount, 0);

    return {
      totalGames: titles.length,
      totalPlayTime: totalSeconds / 3600,
      totalPlayCount,
      avgSessionDuration: totalPlayCount > 0 ? (totalSeconds / totalPlayCount) / 60 : 0, // in minutes
    };
  };

  const getSessionDurationData = (titles: Title[]) => {
    const ranges = [
      { name: "< 30min", min: 0, max: 30, count: 0, color: "#3b82f6" },
      { name: "30-60min", min: 30, max: 60, count: 0, color: "#8b5cf6" },
      { name: "1-2hrs", min: 60, max: 120, count: 0, color: "#ec4899" },
      { name: "2-4hrs", min: 120, max: 240, count: 0, color: "#f59e0b" },
      { name: "> 4hrs", min: 240, max: Infinity, count: 0, color: "#ef4444" },
    ];

    titles
      .filter((title) => title.category.includes("game") && title.playCount > 0)
      .forEach((title) => {
        const avgSessionMins = getDurationInSeconds(title.playDuration) / title.playCount / 60;
        const range = ranges.find((r) => avgSessionMins >= r.min && avgSessionMins < r.max);
        if (range) range.count++;
      });

    return ranges;
  };

  const getGenreData = (titles: Title[]) => {
    const genreMap = new Map<string, { count: number; hours: number }>();

    titles
      .filter((title) => title.category.includes("game"))
      .forEach((title) => {
        const hours = getDurationInHours(title.playDuration);
        title.concept.genres.forEach((genre) => {
          const current = genreMap.get(genre) || { count: 0, hours: 0 };
          genreMap.set(genre, {
            count: current.count + 1,
            hours: current.hours + hours,
          });
        });
      });

    return Array.from(genreMap.entries())
      .map(([name, data]) => ({
        name: shortenString(name),
        games: data.count,
        hours: Math.round(data.hours * 10) / 10,
      }))
      .sort((a, b) => b.hours - a.hours)
      .slice(0, 8);
  };


  const getChartData = (titles: Title[]) => {
    const filteredData = titles
      .filter((title) => title.category.includes("game"))
      .filter((title) => getDurationInHours(title.playDuration) > 0)
      .map((title) => {
        const hours = getDurationInHours(title.playDuration);
        return {
          name: title.name.length > 20 ? title.name.substring(0, 20) + "..." : title.name,
          hours: hours === 0 ? 0.1 : hours,
          display: Math.round(hours * 10) / 10,
          linear: hours,
        };
      })
      .sort((a, b) => a.hours - b.hours);

    const average =
      filteredData.reduce((acc, curr) => acc + curr.hours, 0) / filteredData.length;
    const medianIndex = Math.floor((filteredData.length - 1) / 2);

    return filteredData.map((item, index) => ({
      ...item,
      isMedian: index === medianIndex,
      average,
    }));
  };

  const CustomTooltip = ({ active, payload, label }: any) => {
    if (active && payload && payload.length) {
      return (
        <div className="bg-background p-3 border-2 border-primary/20 rounded-xl shadow-2xl backdrop-blur-sm">
          <p className="font-semibold text-foreground">{label}</p>
          <p className="text-sm text-primary">{`Hours: ${payload[0].payload.display}`}</p>
          {payload[0].payload.isMedian && (
            <p className="text-sm text-green-500 font-medium">📊 Median</p>
          )}
        </div>
      );
    }
    return null;
  };

  const formatPlayDuration = (duration: string) => {
    const seconds = getDurationInSeconds(duration);
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    return `${hours}h ${minutes}m`;
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    const year = date.getFullYear();
    const month = date.toLocaleDateString("en-GB", { month: "short" });
    const day = date.getDate();
    return `${day}${day.nth()} ${month} ${year}`;
  };

  const getAvgSessionTime = (title: Title) => {
    if (title.playCount === 0) return "N/A";
    const avgSeconds = getDurationInSeconds(title.playDuration) / title.playCount;
    const hours = Math.floor(avgSeconds / 3600);
    const minutes = Math.floor((avgSeconds % 3600) / 60);
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  };

  if (error)
    return (
      <div className="text-red-500 text-center py-12 text-xl">Error: {error}</div>
    );
  if (!data)
    return (
      <div className="flex items-center justify-center h-screen">
        <div className="animate-pulse text-2xl">Loading your gaming stats...</div>
      </div>
    );

  const filteredTitles = filterTitles(data.titles);
  const agg = getAggregations(filteredTitles);

  return (
    <div className="w-full min-h-screen bg-gradient-to-br from-slate-950 via-blue-950 to-slate-900">
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
        {/* Header */}
        <div className="mb-8">
          <h1 className="text-5xl font-black bg-gradient-to-r from-blue-400 via-purple-400 to-pink-400 bg-clip-text text-transparent mb-2">
            PlayStation Stats
          </h1>
          <p className="text-gray-400">
            Last updated: {new Date(data.timestamp * 1000).toLocaleString()}
          </p>
        </div>

        {/* Stats Cards */}
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
          <Card className="bg-slate-900 border-l-4 border-l-blue-500 border-slate-700">
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium text-slate-300 flex items-center gap-2">
                <GamepadIcon className="h-4 w-4 text-blue-500" />
                Total Games
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-bold text-white">{agg.totalGames}</div>
            </CardContent>
          </Card>

          <Card className="bg-slate-900 border-l-4 border-l-purple-500 border-slate-700">
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium text-slate-300 flex items-center gap-2">
                <Clock className="h-4 w-4 text-purple-500" />
                Total Hours
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-bold text-white">
                {Math.round(agg.totalPlayTime).toLocaleString()}h
              </div>
            </CardContent>
          </Card>

          <Card className="bg-slate-900 border-l-4 border-l-pink-500 border-slate-700">
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium text-slate-300 flex items-center gap-2">
                <TrendingUp className="h-4 w-4 text-pink-500" />
                Total Sessions
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-bold text-white">
                {agg.totalPlayCount.toLocaleString()}
              </div>
            </CardContent>
          </Card>

          <Card className="bg-slate-900 border-l-4 border-l-orange-500 border-slate-700">
            <CardHeader className="pb-3">
              <CardTitle className="text-sm font-medium text-slate-300 flex items-center gap-2">
                <BarChart3Icon className="h-4 w-4 text-orange-500" />
                Avg Session
              </CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-bold text-white">
                {Math.round(agg.avgSessionDuration)}m
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Milestones & Analytics Selectors */}
        {milestones && (
          <div className="mb-8">
            <h2 className="text-2xl font-bold text-white mb-4">🏆 Milestones</h2>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              {milestones.games200Hours && milestones.games200Hours.length > 0 && (
                <Card className="bg-slate-900 border-l-4 border-l-yellow-500 border-slate-700">
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium text-slate-300">200+ Hours Club</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="text-lg font-bold text-yellow-500 mb-2">{milestones.games200Hours.length} games</div>
                    <div className="text-xs text-slate-400 space-y-1 max-h-20 overflow-y-auto">
                      {milestones.games200Hours.slice(0, 3).map((game, i) => (
                        <div key={i}>• {game}</div>
                      ))}
                      {milestones.games200Hours.length > 3 && (
                        <div>... and {milestones.games200Hours.length - 3} more</div>
                      )}
                    </div>
                  </CardContent>
                </Card>
              )}

              {milestones.games100Hours && milestones.games100Hours.length > 0 && (
                <Card className="bg-slate-900 border-l-4 border-l-emerald-500 border-slate-700">
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium text-slate-300">100+ Hours</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="text-lg font-bold text-emerald-500 mb-2">{milestones.games100Hours.length} games</div>
                    <div className="text-xs text-slate-400 space-y-1 max-h-20 overflow-y-auto">
                      {milestones.games100Hours.slice(0, 3).map((game, i) => (
                        <div key={i}>• {game}</div>
                      ))}
                      {milestones.games100Hours.length > 3 && (
                        <div>... and {milestones.games100Hours.length - 3} more</div>
                      )}
                    </div>
                  </CardContent>
                </Card>
              )}

              {milestones.games50Hours && milestones.games50Hours.length > 0 && (
                <Card className="bg-slate-900 border-l-4 border-l-cyan-500 border-slate-700">
                  <CardHeader className="pb-3">
                    <CardTitle className="text-sm font-medium text-slate-300">50+ Hours</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <div className="text-lg font-bold text-cyan-500 mb-2">{milestones.games50Hours.length} games</div>
                    <div className="text-xs text-slate-400 space-y-1 max-h-20 overflow-y-auto">
                      {milestones.games50Hours.slice(0, 3).map((game, i) => (
                        <div key={i}>• {game}</div>
                      ))}
                      {milestones.games50Hours.length > 3 && (
                        <div>... and {milestones.games50Hours.length - 3} more</div>
                      )}
                    </div>
                  </CardContent>
                </Card>
              )}
            </div>
          </div>
        )}

        {/* Monthly & Yearly Insights */}
        <div className="mb-8">
          <h2 className="text-2xl font-bold text-white mb-4">📊 Detailed Analytics</h2>
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Monthly Games Played */}
            <Card className="bg-slate-900 border-slate-700">
              <CardHeader>
                <CardTitle className="text-white flex items-center justify-between">
                  <span>Games Played This Month</span>
                  <Select value={selectedMonth} onValueChange={setSelectedMonth}>
                    <SelectTrigger className="w-[140px] bg-slate-800 border-slate-700">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {analytics?.monthlyActivity.map((month) => (
                        <SelectItem key={month.month} value={month.month}>
                          {month.month}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </CardTitle>
              </CardHeader>
              <CardContent>
                {monthlyGames && monthlyGames.games && monthlyGames.games.length > 0 ? (
                  <div className="space-y-2 max-h-64 overflow-y-auto">
                    <div className="text-sm text-slate-400 mb-3">
                      Total: <span className="text-white font-semibold">{Math.round(monthlyGames.totalHours)}h</span>
                    </div>
                    {monthlyGames.games.map((game, i) => (
                      <div key={i} className="flex justify-between items-center py-2 border-b border-slate-800">
                        <span className="text-sm text-white truncate flex-1">{game.name}</span>
                        <span className="text-sm text-blue-400 font-semibold ml-2">{Math.round(game.hoursGained)}h</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="text-slate-400">No games played this month</div>
                )}
              </CardContent>
            </Card>

            {/* Yearly Top Games */}
            <Card className="bg-slate-900 border-slate-700">
              <CardHeader>
                <CardTitle className="text-white flex items-center justify-between">
                  <span>Top Games This Year</span>
                  <Select value={String(selectedYear)} onValueChange={(v) => setSelectedYear(Number(v))}>
                    <SelectTrigger className="w-[100px] bg-slate-800 border-slate-700">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {availableYears.map((year) => (
                        <SelectItem key={year} value={String(year)}>
                          {year}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </CardTitle>
              </CardHeader>
              <CardContent>
                {yearlyGames && yearlyGames.games && yearlyGames.games.length > 0 ? (
                  <div className="space-y-2 max-h-64 overflow-y-auto">
                    {yearlyGames.games.map((game, i) => (
                      <div key={i} className="flex justify-between items-center py-2 border-b border-slate-800">
                        <div className="flex items-center gap-2 flex-1 min-w-0">
                          <span className="text-xs text-slate-500 font-mono">#{i + 1}</span>
                          <span className="text-sm text-white truncate">{game.name}</span>
                        </div>
                        <span className="text-sm text-purple-400 font-semibold ml-2">{Math.round(game.hoursGained)}h</span>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="text-slate-400">No games played this year</div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>

        {/* Chart Controls */}
        <div className="flex flex-wrap items-center gap-4 mb-6">
          <Select value={chartType} onValueChange={(v: any) => setChartType(v)}>
            <SelectTrigger className="w-[200px] bg-slate-800/50 border-slate-700">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="distribution">Play Time Distribution</SelectItem>
              <SelectItem value="sessions">Session Duration</SelectItem>
              <SelectItem value="genres">Top Genres</SelectItem>
              <SelectItem value="timeline">🔥 Real Monthly Activity</SelectItem>
              <SelectItem value="topgames">Top 10 Progress</SelectItem>
            </SelectContent>
          </Select>

          {chartType === "distribution" && (
            <Button
              variant="outline"
              className="bg-slate-800/50 border-slate-700"
              onClick={() => setScaleType(scaleType === "log" ? "linear" : "log")}
            >
              {scaleType === "log" ? "📊 Log Scale" : "📈 Linear Scale"}
            </Button>
          )}

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline" className="flex items-center gap-2 bg-slate-800/50 border-slate-700">
                {isNotFiltered() ? (
                  <FilterXIcon className="h-4 w-4" />
                ) : (
                  <FilterIcon className="h-4 w-4" />
                )}
                Filters & Sort
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent className="w-64 bg-slate-900 border-slate-700">
              <DropdownMenuLabel className="text-slate-300">Sort By</DropdownMenuLabel>
              <Select value={sortBy} onValueChange={setSortBy}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="lastPlayed">Last Played</SelectItem>
                  <SelectItem value="mostPlayed">Most Sessions</SelectItem>
                  <SelectItem value="playTime">Total Time</SelectItem>
                  <SelectItem value="avgSession">Avg Session Time</SelectItem>
                  <SelectItem value="name">Name</SelectItem>
                </SelectContent>
              </Select>

              <DropdownMenuSeparator className="bg-slate-700" />
              <DropdownMenuLabel className="text-slate-300">Filter by Genre</DropdownMenuLabel>
              <Select value={filterGenre} onValueChange={setFilterGenre}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Genres</SelectItem>
                  {getAllGenres(data.titles).map((genre) => (
                    <SelectItem key={genre} value={genre}>
                      {shortenString(genre)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              <DropdownMenuSeparator className="bg-slate-700" />
              <DropdownMenuLabel className="text-slate-300">Filter by Service</DropdownMenuLabel>
              <Select value={filterService} onValueChange={setFilterService}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">All Services</SelectItem>
                  {getAllServices(data.titles).map((service) => (
                    <SelectItem key={service} value={service}>
                      {serviceMap.has(service) ? serviceMap.get(service) : service}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>

              {!isNotFiltered() && (
                <>
                  <DropdownMenuSeparator className="bg-slate-700" />
                  <Button
                    variant="outline"
                    className="w-full"
                    onClick={() => {
                      setSortBy("lastPlayed");
                      setFilterGenre("all");
                      setFilterService("all");
                    }}
                  >
                    Clear Filters
                  </Button>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>

        {/* Charts */}
        <Card className="bg-slate-800/30 border-slate-700/50 backdrop-blur-sm mb-8">
          <CardHeader>
            <CardTitle className="text-2xl text-white">
              {chartType === "distribution" && "Play Time Distribution"}
              {chartType === "sessions" && "Average Session Duration"}
              {chartType === "genres" && "Top Genres by Hours"}
              {chartType === "timeline" && "Real Monthly Gaming Activity"}
              {chartType === "topgames" && "Top 10 Games Progress"}
            </CardTitle>
            <CardDescription className="text-slate-400">
              {chartType === "distribution" && "Hours played per game"}
              {chartType === "sessions" && "Distribution by average session length"}
              {chartType === "genres" && "Most played game genres"}
              {chartType === "timeline" && "Actual hours played each month (from daily snapshots)"}
              {chartType === "topgames" && "Monthly progress for your most played games"}
            </CardDescription>
          </CardHeader>
          <CardContent className="w-full h-[500px]">
            <ResponsiveContainer width="100%" height="100%">
              {chartType === "distribution" ? (
                <ComposedChart data={getChartData(filteredTitles)}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
                  <XAxis
                    dataKey="name"
                    angle={-45}
                    textAnchor="end"
                    height={120}
                    interval={0}
                    fontSize={11}
                    stroke="#94a3b8"
                  />
                  <YAxis
                    yAxisId="left"
                    scale={scaleType}
                    domain={scaleType === "log" ? [0.8, "auto"] : [0, "auto"]}
                    tickFormatter={(value) => Math.round(value).toString()}
                    stroke="#94a3b8"
                  />
                  <Tooltip content={<CustomTooltip />} />
                  <Bar yAxisId="left" dataKey="hours" name="Hours Played">
                    {getChartData(filteredTitles).map((entry, index) => (
                      <Cell
                        key={`cell-${index}`}
                        fill={entry.isMedian ? "#f59e0b" : "#3b82f6"}
                        opacity={0.9}
                      />
                    ))}
                  </Bar>
                  <ReferenceLine
                    y={getChartData(filteredTitles)[0]?.average}
                    yAxisId="left"
                    stroke="#ec4899"
                    strokeDasharray="5 5"
                    strokeWidth={2}
                    label={{
                      value: "Average",
                      position: "right",
                      fill: "#ec4899",
                      fontSize: 12,
                    }}
                  />
                </ComposedChart>
              ) : chartType === "sessions" ? (
                <BarChart data={getSessionDurationData(filteredTitles)}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
                  <XAxis dataKey="name" stroke="#94a3b8" />
                  <YAxis stroke="#94a3b8" />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#1e293b",
                      border: "1px solid #475569",
                      borderRadius: "8px",
                    }}
                  />
                  <Bar dataKey="count" name="Games">
                    {getSessionDurationData(filteredTitles).map((entry, index) => (
                      <Cell key={`cell-${index}`} fill={entry.color} />
                    ))}
                  </Bar>
                </BarChart>
              ) : chartType === "genres" ? (
                <RadarChart data={getGenreData(filteredTitles)}>
                  <PolarGrid stroke="#334155" />
                  <PolarAngleAxis dataKey="name" stroke="#94a3b8" />
                  <PolarRadiusAxis stroke="#94a3b8" />
                  <Radar
                    name="Hours"
                    dataKey="hours"
                    stroke="#8b5cf6"
                    fill="#8b5cf6"
                    fillOpacity={0.6}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#1e293b",
                      border: "1px solid #475569",
                      borderRadius: "8px",
                    }}
                  />
                </RadarChart>
              ) : chartType === "timeline" ? (
                <BarChart data={analytics?.monthlyActivity || []}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
                  <XAxis
                    dataKey="month"
                    stroke="#94a3b8"
                    angle={-45}
                    textAnchor="end"
                    height={80}
                  />
                  <YAxis
                    stroke="#94a3b8"
                    label={{ value: 'Hours', angle: -90, position: 'insideLeft', fill: '#94a3b8' }}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#1e293b",
                      border: "1px solid #475569",
                      borderRadius: "8px",
                    }}
                    formatter={(value: number) => [`${Math.round(value)}h`, 'Hours Played']}
                  />
                  <Bar dataKey="hoursPlayed" fill="#3b82f6" radius={[4, 4, 0, 0]}>
                    {(analytics?.monthlyActivity || []).map((entry, index) => (
                      <Cell key={`cell-${index}`} fill={entry.hoursPlayed > 50 ? "#8b5cf6" : "#3b82f6"} />
                    ))}
                  </Bar>
                </BarChart>
              ) : (
                <AreaChart data={analytics?.topGames?.slice(0, 5).map(game => ({
                  name: game.name.substring(0, 20),
                  hours: game.totalHours
                })) || []}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#334155" />
                  <XAxis dataKey="name" stroke="#94a3b8" angle={-45} textAnchor="end" height={100} />
                  <YAxis stroke="#94a3b8" />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#1e293b",
                      border: "1px solid #475569",
                      borderRadius: "8px",
                    }}
                  />
                  <Bar dataKey="hours" fill="#8b5cf6" />
                </AreaChart>
              )}
            </ResponsiveContainer>
          </CardContent>
        </Card>

        {/* Games Grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {sortTitles(filteredTitles)
            .filter((title) => title.category.includes("game"))
            .map((title, index) => (
              <Card
                key={index}
                className="bg-slate-800/40 border-slate-700/50 backdrop-blur-sm hover:bg-slate-800/60 transition-all duration-300 hover:scale-[1.02] hover:shadow-xl hover:shadow-blue-500/10"
              >
                <div className="flex items-center gap-3 p-4 border-b border-slate-700/50">
                  <img
                    src={title.localizedImageUrl}
                    alt={title.name}
                    width={64}
                    height={64}
                    className="rounded-lg shadow-lg"
                    style={{ aspectRatio: "1/1", objectFit: "cover" }}
                  />
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      {title.service === "ps_plus" && (
                        <img src="/ps_plus.svg" alt="PS+" className="h-4 w-4" />
                      )}
                      {title.service === "other" && <DiscIcon className="h-4 w-4" />}
                      <CardTitle className="text-base font-bold text-white truncate">
                        {title.name}
                      </CardTitle>
                    </div>
                    <p className="text-xs text-slate-400 truncate">
                      {title.concept.genres
                        .map((genre) =>
                          genre.includes("_") ? shortenString(genre) : genre
                        )
                        .join(", ")}
                    </p>
                  </div>
                </div>
                <CardContent className="grid grid-cols-2 gap-3 p-4">
                  <div>
                    <p className="text-xs text-slate-400">Sessions</p>
                    <p className="text-lg font-bold text-white">{title.playCount}</p>
                  </div>
                  <div>
                    <p className="text-xs text-slate-400">Total Time</p>
                    <p className="text-lg font-bold text-white">
                      {formatPlayDuration(title.playDuration)}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-slate-400">Avg Session</p>
                    <p className="text-lg font-bold text-blue-400">
                      {getAvgSessionTime(title)}
                    </p>
                  </div>
                  <div>
                    <p className="text-xs text-slate-400">Last Played</p>
                    <p className="text-sm font-medium text-slate-300">
                      {formatDate(title.lastPlayedDateTime)}
                    </p>
                  </div>
                </CardContent>
              </Card>
            ))}
        </div>
      </div>
    </div>
  );

  function isNotFiltered(): boolean {
    return sortBy === "lastPlayed" && filterGenre === "all" && filterService === "all";
  }
}
