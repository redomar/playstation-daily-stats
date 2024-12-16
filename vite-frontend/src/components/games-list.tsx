import { useState, useEffect, useMemo } from "react";
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
import { FilterIcon, FilterXIcon } from "lucide-react";
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

export function GamesList() {
  const [data, setData] = useState<Data | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sortBy, setSortBy] = useState<string>("lastPlayed");
  const [filterGenre, setFilterGenre] = useState<string>("all");
  const [filterService, setFilterService] = useState<string>("all");

  const origins = useMemo(
    () =>
      import.meta.env.VITE_ALLOWED_ORIGINS?.split(",") ?? [
        import.meta.env.VITE_ALLOWED_ORIGINS,
      ],
    []
  );
  const uri = useMemo(() => `${origins[0]}/api/latest-output`, [origins]);

  useEffect(() => {
    const fetchData = async () => {
      try {
        const response = await fetch(uri);
        if (!response.ok) {
          throw new Error("Network response was not ok");
        }
        setData(await response.json());
      } catch (error) {
        console.error("Error fetching data:", error);
        setError("Failed to fetch data");
      }
    };

    fetchData();
  }, [uri]);

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
        case "playTime": {
          const getDurationInSeconds = (duration: string) => {
            const match = duration.match(/PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?/);
            if (!match) return 0;
            const hours = parseInt(match[1] || "0") * 3600;
            const minutes = parseInt(match[2] || "0") * 60;
            const seconds = parseInt(match[3] || "0");
            return hours + minutes + seconds;
          };
          return (
            getDurationInSeconds(b.playDuration) -
            getDurationInSeconds(a.playDuration)
          );
        }
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
    return {
      totalGames: titles.length,
      totalPlayTime: titles.reduce(
        (acc, title) =>
          acc + parseInt(title.playDuration.match(/PT(\d+)H/)?.[1] || "0"),
        0
      ),
      totalPlayCount: titles.reduce((acc, title) => acc + title.playCount, 0),
    };
  };

  const formatPlayDuration = (duration: string) => {
    const match = duration.match(/PT(\d+H)?(\d+M)?(\d+S)?/);
    if (!match) return "Unknown";

    const hours = parseInt(match[1] || "0");
    const minutes = parseInt(match[2] || "0");

    return `${hours}h ${minutes}m`;
  };

  const formatDate = (dateString: string) => {
    const date = new Date(dateString);
    const year = date.getFullYear();
    const month = date.toLocaleDateString("en-GB", { month: "short" });
    const day = date.getDate();

    return `${day}${day.nth()} ${month} ${year}`;
  };

  if (error) return <div className="text-red-500">Error: {error}</div>;
  if (!data) return <div className="text-gray-500">Loading...</div>;

  return (
    <div className="w-full max-w-6xl xl:max-w-screen-2xl px-8 mx-auto py-8">
      <h1 className="text-3xl font-bold mb-6">My Games</h1>
      <div className="mb-6 space-y-4">
        <div className="flex flex-wrap gap-4">
          <Card className="bg-background rounded-lg overflow-hidden">
            <CardHeader>
              <CardTitle>
                <h3>Last Updated</h3>
              </CardTitle>
              <CardDescription>
                {new Date(data.timestamp * 1000).toLocaleString()}
              </CardDescription>
            </CardHeader>
          </Card>
          <Card className="p-4">
            <div className="flex gap-6">
              {(() => {
                const agg = getAggregations(filterTitles(data.titles));
                return (
                  <>
                    <div>
                      <p className="text-sm text-muted-foreground">
                        Total Games
                      </p>
                      <p className="text-lg font-medium">{agg.totalGames}</p>
                    </div>
                    <div>
                      <p className="text-sm text-muted-foreground">
                        Total Play Time
                      </p>
                      <p className="text-lg font-medium">
                        {agg.totalPlayTime}h
                      </p>
                    </div>
                    <div>
                      <p className="text-sm text-muted-foreground">
                        Total Plays
                      </p>
                      <p className="text-lg font-medium">
                        {agg.totalPlayCount}
                      </p>
                    </div>
                  </>
                );
              })()}
            </div>
          </Card>
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="outline" className="flex items-center gap-2">
              {/* <FilterIcon className="h-4 w-4" /> */}
              {isNotFiltered() ? (
                <FilterXIcon className="h-4 w-4" />
              ) : (
                <FilterIcon className="h-4 w-4" />
              )}
              Filters & Sort
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent className="w-56">
            <DropdownMenuLabel>Sort By</DropdownMenuLabel>
            <Select value={sortBy} onValueChange={setSortBy}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="lastPlayed">Last Played</SelectItem>
                <SelectItem value="mostPlayed">Most Played</SelectItem>
                <SelectItem value="playTime">Play Time</SelectItem>
                <SelectItem value="name">Name</SelectItem>
              </SelectContent>
            </Select>

            <DropdownMenuLabel>Filter by Genre</DropdownMenuLabel>
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

            <DropdownMenuLabel>Filter by Service</DropdownMenuLabel>
            <Select value={filterService} onValueChange={setFilterService}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All Services</SelectItem>
                {getAllServices(data.titles).map((service) => (
                  <SelectItem key={service} value={service}>
                    {serviceMap.has(service)
                      ? serviceMap.get(service)
                      : service}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <span className={isNotFiltered()}>
              <DropdownMenuSeparator />
              <DropdownMenuLabel>Clear</DropdownMenuLabel>
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
            </span>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5 xl:gap-4">
        {sortTitles(filterTitles(data.titles))
          .filter((title) => title.category.includes("game"))
          .map((title, index) => (
            <Card
              key={index}
              className="bg-background rounded-lg overflow-hidden"
            >
              <div className="flex items-center gap-4 p-4 xl:p-2 xl:gap-2 border-b">
                <img
                  src={title.localizedImageUrl}
                  alt={title.name}
                  width={80}
                  height={80}
                  className="rounded-md"
                  style={{ aspectRatio: "80/80", objectFit: "cover" }}
                />
                <div className="flex-1">
                  <CardTitle className="text-xl font-bold">
                    <div className="flex flex-row items-center relative">
                      {title.service === "ps_plus" ? (
                        <img
                          src="/ps_plus.svg"
                          alt="PS+"
                          className="size-6 absolute -top-5 left-0"
                        />
                      ) : null}
                      {title.service === "other" ? (
                        <DiscIcon className="size-4 absolute -top-4 left-0" />
                      ) : null}
                      <span className="line-clamp-1 hover:line-clamp-none">
                        {title.name}
                      </span>
                    </div>
                  </CardTitle>
                  <p className="text-sm text-muted-foreground line-clamp-1">
                    {title.concept.genres
                      .map((genre) =>
                        genre.includes("_") ? shortenString(genre) : genre
                      )
                      .join(", ")}
                  </p>
                </div>
              </div>
              <CardContent className="grid grid-cols-2 gap-4 p-4 xl:p-2 xl:gap-2">
                <div>
                  <p className="text-sm text-muted-foreground">Play Count</p>
                  <p className="text-lg font-medium">{title.playCount}</p>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">Play Duration</p>
                  <p className="text-lg font-medium">
                    {formatPlayDuration(title.playDuration)}
                  </p>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">First Played</p>
                  <p className="text-lg font-medium">
                    {formatDate(title.firstPlayedDateTime)}
                  </p>
                </div>
                <div>
                  <p className="text-sm text-muted-foreground">Last Played</p>
                  <p className="text-lg font-medium">
                    {formatDate(title.lastPlayedDateTime)}
                  </p>
                </div>
              </CardContent>
            </Card>
          ))}
      </div>
    </div>
  );

  function isNotFiltered(): string | undefined {
    return sortBy === "lastPlayed" &&
      filterGenre === "all" &&
      filterService === "all"
      ? "hidden"
      : "";
  }
}
