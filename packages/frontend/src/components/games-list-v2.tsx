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
  LineChart,
  Line,
  AreaChart,
  Area,
  Legend,
  Brush,
} from "recharts";

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

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

interface YearlySummary {
  year: number;
  totalHours: number;
  totalSessions: number;
  gamesPlayed: number;
}

interface YoYMonthly {
  month: string;
  years: Record<string, number>;
}

interface YoYComparison {
  summaries: YearlySummary[];
  monthly: YoYMonthly[];
}

interface CumulativePoint {
  month: string;
  cumulativeHours: number;
}

interface GameDeepDive {
  titleId: string;
  name: string;
  totalHours: number;
  totalSessions: number;
  avgSessionMins: number;
  firstSeen: string;
  lastPlayed: string;
  peakMonth: string;
  peakMonthHours: number;
  monthlyData: MonthlyGameData[];
  cumulative: CumulativePoint[];
  genres: string[];
}

interface MonthGenreBreakdown {
  month: string;
  genres: Record<string, number>;
}

interface SessionTrend {
  month: string;
  avgSessionMins: number;
  totalSessions: number;
}

interface GenreShift {
  month: string;
  dominantGenre: string;
}

interface GenreTrends {
  monthlyGenres: MonthGenreBreakdown[];
  sessionTrends: SessionTrend[];
  genreShifts: GenreShift[];
  topGenres: string[];
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
  const [chartType, setChartType] = useState<"distribution" | "sessions" | "genres" | "timeline" | "topgames" | "yoy" | "genretrends">("distribution");
  const [selectedMonth, setSelectedMonth] = useState<string>("");
  const [selectedYear, setSelectedYear] = useState<number>(new Date().getFullYear());
  const [yoyData, setYoyData] = useState<YoYComparison | null>(null);
  const [genreTrends, setGenreTrends] = useState<GenreTrends | null>(null);
  const [gameDeepDive, setGameDeepDive] = useState<GameDeepDive | null>(null);
  const [selectedGameId, setSelectedGameId] = useState<string | null>(null);

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
        const response = await fetch(`${baseUri}/api/latest-output`);
        if (!response.ok) {
          throw new Error("Network response was not ok");
        }
        setData(await response.json());

        const analyticsResponse = await fetch(`${baseUri}/api/analytics`);
        if (analyticsResponse.ok) {
          const analyticsData = await analyticsResponse.json();
          setAnalytics(analyticsData);

          if (analyticsData.monthlyActivity && analyticsData.monthlyActivity.length > 0) {
            const latestMonth = analyticsData.monthlyActivity[analyticsData.monthlyActivity.length - 1].month;
            setSelectedMonth(latestMonth);
          }
        }

        const milestonesResponse = await fetch(`${baseUri}/api/analytics/milestones`);
        if (milestonesResponse.ok) {
          setMilestones(await milestonesResponse.json());
        }

        const yearsResponse = await fetch(`${baseUri}/api/analytics/years`);
        if (yearsResponse.ok) {
          const years = await yearsResponse.json();
          setAvailableYears(years);
          if (years.length > 0) {
            setSelectedYear(years[0]);
          }
        }

        const yoyResponse = await fetch(`${baseUri}/api/analytics/yoy`);
        if (yoyResponse.ok) {
          setYoyData(await yoyResponse.json());
        }

        const genreTrendsResponse = await fetch(`${baseUri}/api/analytics/genre-trends`);
        if (genreTrendsResponse.ok) {
          setGenreTrends(await genreTrendsResponse.json());
        }
      } catch (error) {
        console.error("Error fetching data:", error);
        setError("Failed to fetch data");
      }
    };

    fetchData();
  }, [baseUri]);

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

  useEffect(() => {
    if (!selectedGameId || !baseUri) {
      setGameDeepDive(null);
      return;
    }

    const fetchGameDeepDive = async () => {
      try {
        const response = await fetch(`${baseUri}/api/analytics/game/${selectedGameId}`);
        if (response.ok) {
          setGameDeepDive(await response.json());
        }
      } catch (error) {
        console.error("Error fetching game deep dive:", error);
      }
    };

    fetchGameDeepDive();
  }, [selectedGameId, baseUri]);

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
      avgSessionDuration: totalPlayCount > 0 ? (totalSeconds / totalPlayCount) / 60 : 0,
    };
  };

  const getSessionDurationData = (titles: Title[]) => {
    const ranges = [
      { name: "< 30min", min: 0, max: 30, count: 0, color: "#1A1A1A" },
      { name: "30-60min", min: 30, max: 60, count: 0, color: "#444444" },
      { name: "1-2hrs", min: 60, max: 120, count: 0, color: "#666666" },
      { name: "2-4hrs", min: 120, max: 240, count: 0, color: "#E63312" },
      { name: "> 4hrs", min: 240, max: Infinity, count: 0, color: "#E63312" },
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

  const EditorialTooltip = ({ active, payload, label }: any) => {
    if (active && payload && payload.length) {
      return (
        <div className="bg-paper border-2 border-ink p-3 font-body" style={{ borderRadius: '2px' }}>
          <p className="font-semibold text-ink text-sm">{label}</p>
          <p className="text-sm font-mono-data text-editorial-red font-bold">{payload[0].payload.display}h</p>
          {payload[0].payload.isMedian && (
            <p className="text-xs text-muted font-medium uppercase tracking-wider mt-1">Median</p>
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

  const isNotFiltered = (): boolean => {
    return sortBy === "lastPlayed" && filterGenre === "all" && filterService === "all";
  };

  if (error)
    return (
      <div className="text-editorial-red text-center py-12 font-editorial text-xl">
        Error: {error}
      </div>
    );
  if (!data)
    return (
      <div className="flex items-center justify-center h-screen bg-paper">
        <div className="font-editorial text-3xl text-ink tracking-tight">Loading...</div>
      </div>
    );

  const filteredTitles = filterTitles(data.titles);
  const agg = getAggregations(filteredTitles);

  // Find most recently played game for hero
  const heroGame = [...data.titles]
    .filter((t) => t.category.includes("game"))
    .sort((a, b) => new Date(b.lastPlayedDateTime).getTime() - new Date(a.lastPlayedDateTime).getTime())[0];

  const chartLabels: Record<string, { title: string; desc: string }> = {
    distribution: { title: "Play Time Distribution", desc: "Hours played per game" },
    sessions: { title: "Session Duration", desc: "Distribution by average session length" },
    genres: { title: "Top Genres by Hours", desc: "Most played game genres" },
    timeline: { title: "Monthly Activity", desc: "Full history with zoom — drag the handles below the chart" },
    topgames: { title: "Top 10 Games", desc: "Most played games by total hours" },
    yoy: { title: "Year over Year", desc: "Compare monthly activity across years" },
    genretrends: { title: "Genre & Habit Trends", desc: "Genre breakdown and session patterns over time" },
  };

  return (
    <div className="w-full min-h-screen bg-paper">
      <div className="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8 py-6">

        {/* ─── SECTION A: MASTHEAD ─── */}
        <header className="mb-8">
          <div className="border-t-[3px] border-ink" />
          <div className="flex items-baseline justify-between py-3">
            <h1 className="font-editorial text-3xl sm:text-4xl font-black tracking-tight text-ink uppercase" style={{ letterSpacing: '0.08em' }}>
              PlayStation Stats
            </h1>
            <span className="text-xs sm:text-sm text-muted font-body">
              {new Date(data.timestamp * 1000).toLocaleDateString("en-GB", {
                weekday: "long",
                day: "numeric",
                month: "long",
                year: "numeric",
              })}
            </span>
          </div>
          <div className="border-b-[3px] border-ink" />
          <div className="border-b border-ink mt-[3px]" />
        </header>

        {/* ─── HERO SPOTLIGHT ─── */}
        {heroGame && (
          <section className="mb-8">
            <div className="flex flex-col md:flex-row gap-6">
              {/* Hero image */}
              <div className="md:w-2/3">
                <div className="relative overflow-hidden" style={{ borderRadius: '2px' }}>
                  {heroGame.concept.media.images?.[0]?.url ? (
                    <img
                      src={heroGame.concept.media.images.find(img => img.type === "GAMEHUB_COVER_ART")?.url
                        || heroGame.concept.media.images.find(img => img.type === "MASTER")?.url
                        || heroGame.localizedImageUrl}
                      alt={heroGame.name}
                      className="w-full h-64 sm:h-80 object-cover"
                    />
                  ) : (
                    <img
                      src={heroGame.localizedImageUrl}
                      alt={heroGame.name}
                      className="w-full h-64 sm:h-80 object-cover"
                    />
                  )}
                </div>
              </div>

              {/* Hero data sidebar */}
              <div className="md:w-1/3 flex flex-col justify-between">
                <div>
                  <p className="text-xs text-muted uppercase tracking-widest font-body mb-1">Now Playing</p>
                  <h2 className="font-editorial text-2xl sm:text-3xl font-black text-ink leading-tight mb-3">
                    {heroGame.name}
                  </h2>
                  <p className="text-sm text-muted font-body mb-4">
                    {heroGame.concept.genres.map((g) => shortenString(g)).join(" / ")}
                  </p>
                </div>

                <div className="space-y-3 border-t-2 border-ink pt-3">
                  <div className="flex justify-between items-baseline">
                    <span className="text-xs text-muted uppercase tracking-wider">Total Time</span>
                    <span className="font-mono text-xl font-bold">{formatPlayDuration(heroGame.playDuration)}</span>
                  </div>
                  <div className="flex justify-between items-baseline border-t border-rule pt-2">
                    <span className="text-xs text-muted uppercase tracking-wider">Sessions</span>
                    <span className="font-mono text-xl font-bold">{heroGame.playCount}</span>
                  </div>
                  <div className="flex justify-between items-baseline border-t border-rule pt-2">
                    <span className="text-xs text-muted uppercase tracking-wider">Avg Session</span>
                    <span className="font-mono text-xl font-bold">{getAvgSessionTime(heroGame)}</span>
                  </div>
                  <div className="flex justify-between items-baseline border-t border-rule pt-2">
                    <span className="text-xs text-muted uppercase tracking-wider">Last Played</span>
                    <span className="font-body text-sm">{formatDate(heroGame.lastPlayedDateTime)}</span>
                  </div>
                </div>

                {analytics?.streaks && analytics.streaks.currentStreak > 0 && (
                  <div className="mt-4 border-2 border-ink p-3 text-center">
                    <span className="font-mono text-3xl font-bold text-editorial-red">{analytics.streaks.currentStreak}</span>
                    <span className="text-xs text-muted uppercase tracking-widest ml-2">Day Streak</span>
                  </div>
                )}
              </div>
            </div>
          </section>
        )}

        {/* ─── SECTION B: KEY FIGURES STRIP ─── */}
        <section className="mb-8 border-t-2 border-b-2 border-ink py-4">
          <div className="grid grid-cols-2 md:grid-cols-4">
            {[
              { label: "Total Games", value: agg.totalGames.toLocaleString() },
              { label: "Total Hours", value: `${Math.round(agg.totalPlayTime).toLocaleString()}` },
              { label: "Total Sessions", value: agg.totalPlayCount.toLocaleString() },
              { label: "Avg Session", value: `${Math.round(agg.avgSessionDuration)}m` },
            ].map((stat, i) => (
              <div
                key={stat.label}
                className={`text-center py-2 ${i > 0 ? "border-l-2 border-ink" : ""}`}
              >
                <div className="font-mono text-3xl sm:text-5xl font-bold text-ink leading-none">
                  {stat.value}
                </div>
                <div className="text-xs text-muted uppercase tracking-widest mt-2 font-body">
                  {stat.label}
                </div>
              </div>
            ))}
          </div>
        </section>

        {/* ─── SECTION C: MONTHLY & YEARLY ANALYTICS ─── */}
        <section className="mb-8 border-b-2 border-ink pb-8">
          <div className="flex flex-col lg:flex-row gap-6">
            {/* Monthly Games - 60% */}
            <div className="lg:w-3/5">
              <div className="flex items-baseline justify-between mb-3">
                <h2 className="font-editorial text-xl font-bold text-ink uppercase tracking-wide">
                  Games This Month
                </h2>
                <Select value={selectedMonth} onValueChange={setSelectedMonth}>
                  <SelectTrigger className="w-[130px] border-ink border bg-transparent font-mono text-sm h-8 focus:ring-0">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="bg-paper border-ink border-2">
                    {analytics?.monthlyActivity.map((month) => (
                      <SelectItem key={month.month} value={month.month} className="font-mono text-sm">
                        {month.month}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {monthlyGames && monthlyGames.games && monthlyGames.games.length > 0 ? (
                <div>
                  <div className="text-sm text-muted mb-3 font-body">
                    Total: <span className="text-ink font-mono font-bold">{Math.round(monthlyGames.totalHours)}h</span>
                  </div>
                  {/* Table header */}
                  <div className="grid grid-cols-[1fr_80px_80px] border-b-2 border-ink pb-1 mb-1">
                    <span className="text-xs text-muted uppercase tracking-wider font-body">Game</span>
                    <span className="text-xs text-muted uppercase tracking-wider font-body text-right">Hours</span>
                    <span className="text-xs text-muted uppercase tracking-wider font-body text-right">Sessions</span>
                  </div>
                  <div className="max-h-72 overflow-y-auto">
                    {monthlyGames.games.map((game, i) => (
                      <div
                        key={i}
                        className={`grid grid-cols-[1fr_80px_80px] py-2 border-b border-rule ${i % 2 === 1 ? 'bg-row-alt' : ''}`}
                      >
                        <span className="text-sm text-ink truncate font-body pr-2">{game.name}</span>
                        <span className="text-sm font-mono font-bold text-right">{Math.round(game.hoursGained)}h</span>
                        <span className="text-sm font-mono text-muted text-right">{game.sessionsGained}</span>
                      </div>
                    ))}
                  </div>
                </div>
              ) : (
                <div className="text-muted font-body py-8">No games played this month</div>
              )}
            </div>

            {/* Vertical divider */}
            <div className="hidden lg:block w-[2px] bg-ink" />

            {/* Yearly Top Games - 40% */}
            <div className="lg:w-2/5">
              <div className="flex items-baseline justify-between mb-3">
                <h2 className="font-editorial text-xl font-bold text-ink uppercase tracking-wide">
                  Year in Review
                </h2>
                <Select value={String(selectedYear)} onValueChange={(v) => setSelectedYear(Number(v))}>
                  <SelectTrigger className="w-[90px] border-ink border bg-transparent font-mono text-sm h-8 focus:ring-0">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="bg-paper border-ink border-2">
                    {availableYears.map((year) => (
                      <SelectItem key={year} value={String(year)} className="font-mono text-sm">
                        {year}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              {yearlyGames && yearlyGames.games && yearlyGames.games.length > 0 ? (
                <div className="max-h-72 overflow-y-auto">
                  {yearlyGames.games.map((game, i) => (
                    <div
                      key={i}
                      className={`flex items-baseline gap-3 py-2 border-b border-rule ${i % 2 === 1 ? 'bg-row-alt' : ''}`}
                    >
                      <span className="font-editorial text-2xl font-bold text-ink w-10 shrink-0">
                        #{i + 1}
                      </span>
                      <span className="text-sm text-ink truncate flex-1 font-body">{game.name}</span>
                      <span className="font-mono font-bold text-sm shrink-0">{Math.round(game.hoursGained)}h</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-muted font-body py-8">No data for this year</div>
              )}
            </div>
          </div>
        </section>

        {/* ─── SECTION D: MILESTONES ─── */}
        {milestones && (milestones.games200Hours?.length > 0 || milestones.games100Hours?.length > 0 || milestones.games50Hours?.length > 0) && (
          <section className="mb-8 border-b-2 border-ink pb-8">
            <h2 className="font-editorial text-xl font-bold text-ink uppercase tracking-wide mb-4">
              Milestones
            </h2>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
              {milestones.games200Hours && milestones.games200Hours.length > 0 && (
                <div className="border-l-[3px] border-ink pl-4">
                  <div className="font-mono text-4xl font-bold text-ink">{milestones.games200Hours.length}</div>
                  <div className="text-xs text-muted uppercase tracking-widest mt-1 mb-3 font-body">200+ Hours</div>
                  <div className="space-y-1">
                    {milestones.games200Hours.map((game, i) => (
                      <div key={i} className="text-sm font-body text-ink">{game}</div>
                    ))}
                  </div>
                </div>
              )}

              {milestones.games100Hours && milestones.games100Hours.length > 0 && (
                <div className="border-l-[3px] border-ink pl-4">
                  <div className="font-mono text-4xl font-bold text-ink">{milestones.games100Hours.length}</div>
                  <div className="text-xs text-muted uppercase tracking-widest mt-1 mb-3 font-body">100+ Hours</div>
                  <div className="space-y-1">
                    {milestones.games100Hours.slice(0, 5).map((game, i) => (
                      <div key={i} className="text-sm font-body text-ink">{game}</div>
                    ))}
                    {milestones.games100Hours.length > 5 && (
                      <div className="text-xs text-muted">+{milestones.games100Hours.length - 5} more</div>
                    )}
                  </div>
                </div>
              )}

              {milestones.games50Hours && milestones.games50Hours.length > 0 && (
                <div className="border-l-[3px] border-ink pl-4">
                  <div className="font-mono text-4xl font-bold text-ink">{milestones.games50Hours.length}</div>
                  <div className="text-xs text-muted uppercase tracking-widest mt-1 mb-3 font-body">50+ Hours</div>
                  <div className="space-y-1">
                    {milestones.games50Hours.slice(0, 5).map((game, i) => (
                      <div key={i} className="text-sm font-body text-ink">{game}</div>
                    ))}
                    {milestones.games50Hours.length > 5 && (
                      <div className="text-xs text-muted">+{milestones.games50Hours.length - 5} more</div>
                    )}
                  </div>
                </div>
              )}
            </div>
          </section>
        )}

        {/* ─── SECTION E: CHARTS ─── */}
        <section className="mb-8 border-b-2 border-ink pb-8">
          <div className="flex items-baseline justify-between mb-1">
            <h2 className="font-editorial text-xl font-bold text-ink uppercase tracking-wide">
              {chartLabels[chartType].title}
            </h2>
          </div>
          <p className="text-sm text-muted font-body mb-4">{chartLabels[chartType].desc}</p>

          {/* Chart selector as inline text links */}
          <div className="flex flex-wrap gap-4 mb-6 text-sm font-body">
            {(["distribution", "sessions", "genres", "timeline", "topgames", "yoy", "genretrends"] as const).map((type) => (
              <button
                key={type}
                onClick={() => setChartType(type)}
                className={`pb-1 transition-colors ${
                  chartType === type
                    ? "border-b-2 border-ink text-ink font-semibold"
                    : "text-muted hover:text-ink"
                }`}
              >
                {chartLabels[type].title}
              </button>
            ))}

            {chartType === "distribution" && (
              <button
                onClick={() => setScaleType(scaleType === "log" ? "linear" : "log")}
                className="text-muted hover:text-ink ml-auto text-xs uppercase tracking-wider"
              >
                [{scaleType === "log" ? "Log" : "Linear"} Scale]
              </button>
            )}
          </div>

          <div className="w-full h-[400px]">
            <ResponsiveContainer width="100%" height="100%">
              {chartType === "distribution" ? (
                <ComposedChart data={getChartData(filteredTitles)}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                  <XAxis
                    dataKey="name"
                    angle={-45}
                    textAnchor="end"
                    height={120}
                    interval={0}
                    fontSize={10}
                    stroke="#999999"
                    fontFamily="JetBrains Mono"
                  />
                  <YAxis
                    yAxisId="left"
                    scale={scaleType}
                    domain={scaleType === "log" ? [0.8, "auto"] : [0, "auto"]}
                    tickFormatter={(value) => Math.round(value).toString()}
                    stroke="#999999"
                    fontFamily="JetBrains Mono"
                    fontSize={11}
                  />
                  <Tooltip content={<EditorialTooltip />} />
                  <Bar yAxisId="left" dataKey="hours" name="Hours Played">
                    {getChartData(filteredTitles).map((entry, index) => (
                      <Cell
                        key={`cell-${index}`}
                        fill={entry.isMedian ? "#E63312" : "#1A1A1A"}
                        opacity={0.85}
                      />
                    ))}
                  </Bar>
                  <ReferenceLine
                    y={getChartData(filteredTitles)[0]?.average}
                    yAxisId="left"
                    stroke="#E63312"
                    strokeDasharray="5 5"
                    strokeWidth={2}
                    label={{
                      value: "Average",
                      position: "right",
                      fill: "#E63312",
                      fontSize: 11,
                      fontFamily: "DM Sans",
                    }}
                  />
                </ComposedChart>
              ) : chartType === "sessions" ? (
                <BarChart data={getSessionDurationData(filteredTitles)}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                  <XAxis dataKey="name" stroke="#999999" fontFamily="JetBrains Mono" fontSize={11} />
                  <YAxis stroke="#999999" fontFamily="JetBrains Mono" fontSize={11} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#FAFAF8",
                      border: "2px solid #1A1A1A",
                      borderRadius: "2px",
                      fontFamily: "DM Sans",
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
                  <PolarGrid stroke="#E0E0DC" />
                  <PolarAngleAxis dataKey="name" stroke="#1A1A1A" fontSize={11} fontFamily="DM Sans" />
                  <PolarRadiusAxis stroke="#999999" fontSize={10} fontFamily="JetBrains Mono" />
                  <Radar
                    name="Hours"
                    dataKey="hours"
                    stroke="#1A1A1A"
                    fill="#1A1A1A"
                    fillOpacity={0.15}
                    strokeWidth={2}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#FAFAF8",
                      border: "2px solid #1A1A1A",
                      borderRadius: "2px",
                      fontFamily: "DM Sans",
                    }}
                  />
                </RadarChart>
              ) : chartType === "timeline" ? (
                <BarChart data={analytics?.monthlyActivity || []}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                  <XAxis
                    dataKey="month"
                    stroke="#999999"
                    angle={-45}
                    textAnchor="end"
                    height={80}
                    fontFamily="JetBrains Mono"
                    fontSize={10}
                  />
                  <YAxis
                    stroke="#999999"
                    fontFamily="JetBrains Mono"
                    fontSize={11}
                    label={{ value: 'Hours', angle: -90, position: 'insideLeft', fill: '#999999', fontFamily: 'DM Sans', fontSize: 12 }}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#FAFAF8",
                      border: "2px solid #1A1A1A",
                      borderRadius: "2px",
                      fontFamily: "DM Sans",
                    }}
                    formatter={(value: number) => [`${Math.round(value)}h`, 'Hours Played']}
                  />
                  <Bar dataKey="hoursPlayed" fill="#1A1A1A" radius={[1, 1, 0, 0]}>
                    {(analytics?.monthlyActivity || []).map((entry, index) => (
                      <Cell key={`cell-${index}`} fill={entry.hoursPlayed > 50 ? "#E63312" : "#1A1A1A"} />
                    ))}
                  </Bar>
                  <Brush
                    dataKey="month"
                    height={30}
                    stroke="#1A1A1A"
                    fill="#F5F5F3"
                    startIndex={Math.max(0, (analytics?.monthlyActivity?.length || 12) - 12)}
                    travellerWidth={8}
                  />
                </BarChart>
              ) : chartType === "topgames" ? (
                <BarChart data={analytics?.topGames?.slice(0, 10).map(game => ({
                  name: game.name.length > 18 ? game.name.substring(0, 18) + "..." : game.name,
                  hours: Math.round(game.totalHours),
                })) || []}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                  <XAxis dataKey="name" stroke="#999999" angle={-45} textAnchor="end" height={100} fontFamily="JetBrains Mono" fontSize={10} />
                  <YAxis stroke="#999999" fontFamily="JetBrains Mono" fontSize={11} />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#FAFAF8",
                      border: "2px solid #1A1A1A",
                      borderRadius: "2px",
                      fontFamily: "DM Sans",
                    }}
                    formatter={(value: number) => [`${value}h`, 'Hours']}
                  />
                  <Bar dataKey="hours" fill="#1A1A1A">
                    {(analytics?.topGames?.slice(0, 10) || []).map((_, index) => (
                      <Cell key={`cell-${index}`} fill={index === 0 ? "#E63312" : "#1A1A1A"} />
                    ))}
                  </Bar>
                </BarChart>
              ) : chartType === "yoy" ? (
                <LineChart data={(() => {
                  if (!yoyData?.monthly) return [];
                  return yoyData.monthly.map(m => ({
                    month: m.month,
                    ...m.years,
                  }));
                })()}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                  <XAxis dataKey="month" stroke="#999999" fontFamily="JetBrains Mono" fontSize={11} />
                  <YAxis stroke="#999999" fontFamily="JetBrains Mono" fontSize={11}
                    label={{ value: 'Hours', angle: -90, position: 'insideLeft', fill: '#999999', fontFamily: 'DM Sans', fontSize: 12 }}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#FAFAF8",
                      border: "2px solid #1A1A1A",
                      borderRadius: "2px",
                      fontFamily: "DM Sans",
                    }}
                    formatter={(value: number, name: string) => [`${Math.round(value)}h`, name]}
                  />
                  <Legend iconType="plainline" wrapperStyle={{ fontFamily: 'DM Sans', fontSize: 12 }} />
                  {yoyData?.summaries?.map((s, i) => {
                    const colors = ["#1A1A1A", "#E63312", "#0055FF", "#999999", "#666666"];
                    return (
                      <Line
                        key={s.year}
                        type="monotone"
                        dataKey={String(s.year)}
                        stroke={colors[i % colors.length]}
                        strokeWidth={i === (yoyData.summaries.length - 1) ? 3 : 2}
                        dot={false}
                        connectNulls
                      />
                    );
                  })}
                </LineChart>
              ) : chartType === "genretrends" ? (
                <AreaChart data={(() => {
                  if (!genreTrends?.monthlyGenres) return [];
                  return genreTrends.monthlyGenres.map(mg => ({
                    month: mg.month,
                    ...mg.genres,
                  }));
                })()}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                  <XAxis dataKey="month" stroke="#999999" angle={-45} textAnchor="end" height={80} fontFamily="JetBrains Mono" fontSize={10} />
                  <YAxis stroke="#999999" fontFamily="JetBrains Mono" fontSize={11}
                    label={{ value: 'Hours', angle: -90, position: 'insideLeft', fill: '#999999', fontFamily: 'DM Sans', fontSize: 12 }}
                  />
                  <Tooltip
                    contentStyle={{
                      backgroundColor: "#FAFAF8",
                      border: "2px solid #1A1A1A",
                      borderRadius: "2px",
                      fontFamily: "DM Sans",
                    }}
                    formatter={(value: number, name: string) => [`${Math.round(value * 10) / 10}h`, name]}
                  />
                  <Legend iconType="plainline" wrapperStyle={{ fontFamily: 'DM Sans', fontSize: 12 }} />
                  {genreTrends?.topGenres?.map((genre, i) => {
                    const colors = ["#1A1A1A", "#E63312", "#0055FF", "#999999", "#666666"];
                    return (
                      <Area
                        key={genre}
                        type="monotone"
                        dataKey={genre}
                        stackId="1"
                        stroke={colors[i % colors.length]}
                        fill={colors[i % colors.length]}
                        fillOpacity={0.15 + (i * 0.05)}
                      />
                    );
                  })}
                  <Brush dataKey="month" height={30} stroke="#1A1A1A" fill="#F5F5F3" travellerWidth={8} />
                </AreaChart>
              ) : (
                <BarChart data={[]}><Bar dataKey="x" /></BarChart>
              )}
            </ResponsiveContainer>
          </div>

          {/* YoY Year Summary Strip */}
          {chartType === "yoy" && yoyData?.summaries && (
            <div className="flex flex-wrap gap-3 mt-4">
              {yoyData.summaries.map((s, i) => {
                const colors = ["#1A1A1A", "#E63312", "#0055FF", "#999999", "#666666"];
                return (
                  <div key={s.year} className="border-2 border-rule px-4 py-2 text-center" style={{ borderLeftColor: colors[i % colors.length], borderLeftWidth: 4 }}>
                    <div className="font-editorial text-lg font-bold">{s.year}</div>
                    <div className="font-mono text-sm font-bold">{Math.round(s.totalHours)}h</div>
                    <div className="text-xs text-muted">{s.totalSessions} sessions</div>
                    <div className="text-xs text-muted">{s.gamesPlayed} games</div>
                  </div>
                );
              })}
            </div>
          )}

          {/* Genre Trends: Session length trend + dominant genre strip */}
          {chartType === "genretrends" && genreTrends && (
            <div className="mt-6 space-y-4">
              <h3 className="font-editorial text-lg font-bold text-ink uppercase tracking-wide">
                Avg Session Length Trend
              </h3>
              <div className="w-full h-[200px]">
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={genreTrends.sessionTrends}>
                    <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                    <XAxis dataKey="month" stroke="#999999" angle={-45} textAnchor="end" height={60} fontFamily="JetBrains Mono" fontSize={9} />
                    <YAxis stroke="#999999" fontFamily="JetBrains Mono" fontSize={11}
                      label={{ value: 'Minutes', angle: -90, position: 'insideLeft', fill: '#999999', fontFamily: 'DM Sans', fontSize: 11 }}
                    />
                    <Tooltip
                      contentStyle={{ backgroundColor: "#FAFAF8", border: "2px solid #1A1A1A", borderRadius: "2px", fontFamily: "DM Sans" }}
                      formatter={(value: number) => [`${Math.round(value)}m`, 'Avg Session']}
                    />
                    <Line type="monotone" dataKey="avgSessionMins" stroke="#E63312" strokeWidth={2} dot={false} />
                  </LineChart>
                </ResponsiveContainer>
              </div>

              <h3 className="font-editorial text-lg font-bold text-ink uppercase tracking-wide">
                Dominant Genre by Month
              </h3>
              <div className="flex flex-wrap gap-1">
                {genreTrends.genreShifts.map((gs) => (
                  <div key={gs.month} className="border border-rule px-2 py-1 text-center" style={{ minWidth: 60 }}>
                    <div className="text-[9px] text-muted font-mono">{gs.month}</div>
                    <div className="text-[10px] font-body font-medium truncate">{shortenString(gs.dominantGenre)}</div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </section>

        {/* ─── SECTION F: GAME LIBRARY ─── */}
        <section>
          <div className="flex items-baseline justify-between mb-4">
            <h2 className="font-editorial text-xl font-bold text-ink uppercase tracking-wide">
              Library
            </h2>
            <span className="text-sm text-muted font-mono">{filteredTitles.filter(t => t.category.includes("game")).length} titles</span>
          </div>

          {/* Filter & Sort Controls */}
          <div className="flex flex-wrap items-center gap-3 mb-4 text-sm font-body">
            {/* Sort links */}
            <span className="text-xs text-muted uppercase tracking-wider mr-1">Sort:</span>
            {([
              { key: "lastPlayed", label: "Recent" },
              { key: "playTime", label: "Time" },
              { key: "mostPlayed", label: "Sessions" },
              { key: "avgSession", label: "Avg" },
              { key: "name", label: "A-Z" },
            ] as const).map(({ key, label }) => (
              <button
                key={key}
                onClick={() => setSortBy(key)}
                className={`pb-0.5 transition-colors ${
                  sortBy === key
                    ? "border-b-2 border-ink text-ink font-semibold"
                    : "text-muted hover:text-ink"
                }`}
              >
                {label}
              </button>
            ))}

            <span className="text-rule mx-2">|</span>

            {/* Genre filter */}
            <span className="text-xs text-muted uppercase tracking-wider mr-1">Genre:</span>
            <Select value={filterGenre} onValueChange={setFilterGenre}>
              <SelectTrigger className="w-[120px] border-ink border bg-transparent font-body text-sm h-7 focus:ring-0">
                <SelectValue />
              </SelectTrigger>
              <SelectContent className="bg-paper border-ink border-2">
                <SelectItem value="all" className="font-body text-sm">All</SelectItem>
                {getAllGenres(data.titles).map((genre) => (
                  <SelectItem key={genre} value={genre} className="font-body text-sm">
                    {shortenString(genre)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {/* Service filter */}
            <span className="text-xs text-muted uppercase tracking-wider mr-1">Service:</span>
            <Select value={filterService} onValueChange={setFilterService}>
              <SelectTrigger className="w-[140px] border-ink border bg-transparent font-body text-sm h-7 focus:ring-0">
                <SelectValue />
              </SelectTrigger>
              <SelectContent className="bg-paper border-ink border-2">
                <SelectItem value="all" className="font-body text-sm">All</SelectItem>
                {getAllServices(data.titles).map((service) => (
                  <SelectItem key={service} value={service} className="font-body text-sm">
                    {serviceMap.has(service) ? serviceMap.get(service) : service}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            {!isNotFiltered() && (
              <button
                onClick={() => {
                  setSortBy("lastPlayed");
                  setFilterGenre("all");
                  setFilterService("all");
                }}
                className="text-editorial-red hover:underline text-xs uppercase tracking-wider ml-2"
              >
                Clear Filters
              </button>
            )}
          </div>

          {/* Desktop Table */}
          <div className="hidden md:block">
            {/* Table header */}
            <div className="grid grid-cols-[40px_1fr_120px_80px_100px_90px_120px] gap-2 border-b-2 border-ink pb-2 mb-1">
              <span></span>
              <span className="text-xs text-muted uppercase tracking-wider font-body">Name</span>
              <span className="text-xs text-muted uppercase tracking-wider font-body">Genre</span>
              <span className="text-xs text-muted uppercase tracking-wider font-body text-right">Sessions</span>
              <span className="text-xs text-muted uppercase tracking-wider font-body text-right">Total Time</span>
              <span className="text-xs text-muted uppercase tracking-wider font-body text-right">Avg</span>
              <span className="text-xs text-muted uppercase tracking-wider font-body text-right">Last Played</span>
            </div>

            {/* Table rows */}
            {sortTitles(filteredTitles)
              .filter((title) => title.category.includes("game"))
              .map((title, index) => (
                <div key={title.titleId}>
                  <div
                    onClick={() => setSelectedGameId(selectedGameId === title.titleId ? null : title.titleId)}
                    className={`grid grid-cols-[40px_1fr_120px_80px_100px_90px_120px] gap-2 py-2 border-b border-rule items-center group hover:border-l-[3px] hover:border-l-editorial-red hover:pl-1 transition-all cursor-pointer ${
                      index % 2 === 1 ? "bg-row-alt" : ""
                    } ${selectedGameId === title.titleId ? "border-l-[3px] border-l-editorial-red pl-1" : ""}`}
                  >
                    <img
                      src={title.localizedImageUrl}
                      alt={title.name}
                      className="w-10 h-10 object-cover"
                      style={{ borderRadius: '2px' }}
                    />
                    <div className="truncate">
                      <span className="text-sm font-body text-ink font-medium">{title.name}</span>
                    </div>
                    <span className="text-xs text-muted font-body truncate">
                      {title.concept.genres.map((g) => shortenString(g)).join(", ")}
                    </span>
                    <span className="text-sm font-mono text-right">{title.playCount}</span>
                    <span className="text-sm font-mono font-bold text-right">
                      {formatPlayDuration(title.playDuration)}
                    </span>
                    <span className="text-sm font-mono text-right text-muted">
                      {getAvgSessionTime(title)}
                    </span>
                    <span className="text-xs font-body text-muted text-right">
                      {formatDate(title.lastPlayedDateTime)}
                    </span>
                  </div>

                  {/* Inline Deep Dive Panel */}
                  {selectedGameId === title.titleId && gameDeepDive && (
                    <div className="border-l-[3px] border-editorial-red bg-row-alt p-4 mb-1">
                      <div className="flex items-center justify-between mb-4">
                        <h3 className="font-editorial text-lg font-bold text-ink">{gameDeepDive.name}</h3>
                        <button
                          onClick={(e) => { e.stopPropagation(); setSelectedGameId(null); }}
                          className="text-muted hover:text-ink text-xs uppercase tracking-wider"
                        >
                          Close
                        </button>
                      </div>

                      {/* Stats strip */}
                      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4 border-t-2 border-b-2 border-ink py-3">
                        <div className="text-center">
                          <div className="font-mono text-2xl font-bold">{Math.round(gameDeepDive.totalHours)}h</div>
                          <div className="text-xs text-muted uppercase tracking-wider">Total</div>
                        </div>
                        <div className="text-center">
                          <div className="font-mono text-2xl font-bold">{gameDeepDive.totalSessions}</div>
                          <div className="text-xs text-muted uppercase tracking-wider">Sessions</div>
                        </div>
                        <div className="text-center">
                          <div className="font-mono text-2xl font-bold">{Math.round(gameDeepDive.avgSessionMins)}m</div>
                          <div className="text-xs text-muted uppercase tracking-wider">Avg Session</div>
                        </div>
                        <div className="text-center">
                          <div className="font-mono text-2xl font-bold">{gameDeepDive.peakMonth}</div>
                          <div className="text-xs text-muted uppercase tracking-wider">Peak Month</div>
                        </div>
                      </div>

                      <div className="grid grid-cols-2 gap-2 mb-4 text-sm font-body">
                        <div><span className="text-muted">First seen:</span> <span className="font-mono">{gameDeepDive.firstSeen}</span></div>
                        <div><span className="text-muted">Last played:</span> <span className="font-mono">{gameDeepDive.lastPlayed}</span></div>
                        {gameDeepDive.genres && gameDeepDive.genres.length > 0 && (
                          <div className="col-span-2"><span className="text-muted">Genres:</span> {gameDeepDive.genres.join(", ")}</div>
                        )}
                      </div>

                      {/* Monthly hours bar chart */}
                      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                        <div>
                          <h4 className="text-xs text-muted uppercase tracking-wider mb-2 font-body">Monthly Hours</h4>
                          <div className="h-[200px]">
                            <ResponsiveContainer width="100%" height="100%">
                              <BarChart data={gameDeepDive.monthlyData}>
                                <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                                <XAxis dataKey="month" stroke="#999999" fontSize={9} fontFamily="JetBrains Mono" angle={-45} textAnchor="end" height={60} />
                                <YAxis stroke="#999999" fontSize={10} fontFamily="JetBrains Mono" />
                                <Tooltip
                                  contentStyle={{ backgroundColor: "#FAFAF8", border: "2px solid #1A1A1A", borderRadius: "2px", fontFamily: "DM Sans" }}
                                  formatter={(value: number) => [`${Math.round(value * 10) / 10}h`, 'Hours']}
                                />
                                <Bar dataKey="hours" fill="#1A1A1A" radius={[1, 1, 0, 0]}>
                                  {gameDeepDive.monthlyData.map((entry, i) => (
                                    <Cell key={i} fill={entry.month === gameDeepDive.peakMonth ? "#E63312" : "#1A1A1A"} />
                                  ))}
                                </Bar>
                              </BarChart>
                            </ResponsiveContainer>
                          </div>
                        </div>

                        {/* Cumulative progress area chart */}
                        <div>
                          <h4 className="text-xs text-muted uppercase tracking-wider mb-2 font-body">Cumulative Progress</h4>
                          <div className="h-[200px]">
                            <ResponsiveContainer width="100%" height="100%">
                              <AreaChart data={gameDeepDive.cumulative}>
                                <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                                <XAxis dataKey="month" stroke="#999999" fontSize={9} fontFamily="JetBrains Mono" angle={-45} textAnchor="end" height={60} />
                                <YAxis stroke="#999999" fontSize={10} fontFamily="JetBrains Mono" />
                                <Tooltip
                                  contentStyle={{ backgroundColor: "#FAFAF8", border: "2px solid #1A1A1A", borderRadius: "2px", fontFamily: "DM Sans" }}
                                  formatter={(value: number) => [`${Math.round(value * 10) / 10}h`, 'Total Hours']}
                                />
                                <Area type="monotone" dataKey="cumulativeHours" stroke="#1A1A1A" fill="#1A1A1A" fillOpacity={0.1} strokeWidth={2} />
                              </AreaChart>
                            </ResponsiveContainer>
                          </div>
                        </div>
                      </div>
                    </div>
                  )}

                  {selectedGameId === title.titleId && !gameDeepDive && (
                    <div className="border-l-[3px] border-editorial-red bg-row-alt p-4 mb-1 text-center">
                      <span className="font-body text-muted text-sm">Loading game data...</span>
                    </div>
                  )}
                </div>
              ))}
          </div>

          {/* Mobile Card Layout */}
          <div className="md:hidden space-y-3">
            {sortTitles(filteredTitles)
              .filter((title) => title.category.includes("game"))
              .map((title) => (
                <div key={title.titleId}>
                  <div
                    onClick={() => setSelectedGameId(selectedGameId === title.titleId ? null : title.titleId)}
                    className={`border border-rule p-3 flex gap-3 cursor-pointer ${
                      selectedGameId === title.titleId ? "border-l-[3px] border-l-editorial-red" : ""
                    }`}
                    style={{ borderRadius: '2px' }}
                  >
                    <img
                      src={title.localizedImageUrl}
                      alt={title.name}
                      className="w-14 h-14 object-cover shrink-0"
                      style={{ borderRadius: '2px' }}
                    />
                    <div className="flex-1 min-w-0">
                      <div className="text-sm font-body font-medium text-ink truncate">{title.name}</div>
                      <div className="text-xs text-muted font-body mb-2">
                        {title.concept.genres.map((g) => shortenString(g)).join(", ")}
                      </div>
                      <div className="grid grid-cols-3 gap-2">
                        <div>
                          <div className="text-xs text-muted uppercase">Time</div>
                          <div className="text-sm font-mono font-bold">{formatPlayDuration(title.playDuration)}</div>
                        </div>
                        <div>
                          <div className="text-xs text-muted uppercase">Sessions</div>
                          <div className="text-sm font-mono">{title.playCount}</div>
                        </div>
                        <div>
                          <div className="text-xs text-muted uppercase">Avg</div>
                          <div className="text-sm font-mono">{getAvgSessionTime(title)}</div>
                        </div>
                      </div>
                    </div>
                  </div>

                  {/* Mobile Deep Dive Panel */}
                  {selectedGameId === title.titleId && gameDeepDive && (
                    <div className="border-l-[3px] border-editorial-red bg-row-alt p-3 mt-1" style={{ borderRadius: '2px' }}>
                      <div className="flex items-center justify-between mb-3">
                        <h3 className="font-editorial text-base font-bold text-ink">{gameDeepDive.name}</h3>
                        <button
                          onClick={(e) => { e.stopPropagation(); setSelectedGameId(null); }}
                          className="text-muted hover:text-ink text-xs uppercase tracking-wider"
                        >
                          Close
                        </button>
                      </div>

                      <div className="grid grid-cols-2 gap-3 mb-3 border-t border-b border-rule py-2">
                        <div className="text-center">
                          <div className="font-mono text-xl font-bold">{Math.round(gameDeepDive.totalHours)}h</div>
                          <div className="text-[10px] text-muted uppercase">Total</div>
                        </div>
                        <div className="text-center">
                          <div className="font-mono text-xl font-bold">{Math.round(gameDeepDive.avgSessionMins)}m</div>
                          <div className="text-[10px] text-muted uppercase">Avg Session</div>
                        </div>
                      </div>

                      <div className="text-xs font-body space-y-1 mb-3">
                        <div><span className="text-muted">Peak:</span> <span className="font-mono">{gameDeepDive.peakMonth}</span></div>
                        <div><span className="text-muted">First seen:</span> <span className="font-mono">{gameDeepDive.firstSeen}</span></div>
                        <div><span className="text-muted">Last played:</span> <span className="font-mono">{gameDeepDive.lastPlayed}</span></div>
                      </div>

                      <div className="h-[180px]">
                        <ResponsiveContainer width="100%" height="100%">
                          <BarChart data={gameDeepDive.monthlyData}>
                            <CartesianGrid strokeDasharray="3 3" stroke="#E0E0DC" />
                            <XAxis dataKey="month" stroke="#999999" fontSize={8} fontFamily="JetBrains Mono" angle={-45} textAnchor="end" height={50} />
                            <YAxis stroke="#999999" fontSize={9} fontFamily="JetBrains Mono" />
                            <Bar dataKey="hours" fill="#1A1A1A" radius={[1, 1, 0, 0]}>
                              {gameDeepDive.monthlyData.map((entry, i) => (
                                <Cell key={i} fill={entry.month === gameDeepDive.peakMonth ? "#E63312" : "#1A1A1A"} />
                              ))}
                            </Bar>
                          </BarChart>
                        </ResponsiveContainer>
                      </div>
                    </div>
                  )}

                  {selectedGameId === title.titleId && !gameDeepDive && (
                    <div className="border-l-[3px] border-editorial-red bg-row-alt p-3 mt-1 text-center" style={{ borderRadius: '2px' }}>
                      <span className="font-body text-muted text-sm">Loading game data...</span>
                    </div>
                  )}
                </div>
              ))}
          </div>
        </section>

        {/* Footer */}
        <footer className="mt-8 pt-4 border-t-[3px] border-ink">
          <div className="border-t border-ink mt-[3px]" />
          <div className="flex justify-between items-center pt-3 pb-6">
            <span className="text-xs text-muted font-body">PSN Stats</span>
            <span className="text-xs text-muted font-mono">
              {data.totalItemCount} titles tracked
            </span>
          </div>
        </footer>
      </div>
    </div>
  );
}
