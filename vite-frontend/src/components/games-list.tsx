import { useState, useEffect } from "react";
import {
  Card,
  CardContent,
  CardTitle,
} from "@/components/ui/card";

import { DiscIcon } from '@radix-ui/react-icons'

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
}

function shortenString(str: string): string {
  // Check if the string contains an underscore
  if (str.includes("_")) {
    return str
      .split("_") // Split the string by underscores
      .map((word) => word[0]) // Take the first letter of each word
      .join(""); // Join them together to form the shortened string
  }

  // If no underscore, return the original string unchanged
  return str;
}

export function GamesList() {
  const [data, setData] = useState<Data | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    fetch("http://localhost:8080/api/latest-output")
      .then((response) => {
        if (!response.ok) {
          throw new Error("Network response was not ok");
        }
        return response.json();
      })
      .then((data) => setData(data))
      .catch((error) => {
        console.error("Error fetching data:", error);
        setError("Failed to fetch data");
      });
  }, []);

  const formatPlayDuration = (duration: string) => {
    const match = duration.match(/PT(\d+H)?(\d+M)?(\d+S)?/);
    if (!match) return "Unknown";

    const hours = parseInt(match[1] || "0");
    const minutes = parseInt(match[2] || "0");

    return `${hours}h ${minutes}m`;
  };

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString();
  };

  if (error) return <div className="text-red-500">Error: {error}</div>;
  if (!data) return <div className="text-gray-500">Loading...</div>;

  return (
    <div className="w-full max-w-6xl mx-auto py-8">
      <h1 className="text-3xl font-bold mb-6">My Games</h1>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
        {data.titles
          .filter((title) => title.category.includes("game"))
          .map((title, index) => (
            <Card
              key={index}
              className="bg-background rounded-lg shadow-md  overflow-hidden"
            >
              <div className="flex items-center gap-4 p-4 border-b">
                <img
                  src={title.localizedImageUrl}
                  alt={title.name}
                  width={80}
                  height={80}
                  className="rounded-md"
                  style={{ aspectRatio: "80/80", objectFit: "cover" }}
                />
                <div className="flex-1">
                  <CardTitle className="text-xl font-bold ">
                    <div className="flex flex-row items-center relative">
                      {title.service === "ps_plus" ? (
                        <img src="/ps_plus.svg" alt="PS+" className="size-6 absolute -top-5 left-0" />
                      ) : null}
                      {title.service === "other" ? (
                        <DiscIcon className="size-4 absolute  -top-4 left-0" />
                      ) : null}
                      <span className=" line-clamp-1">{title.name}</span>{" "}
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
              <CardContent className="grid grid-cols-2 gap-4 p-4">
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
}
