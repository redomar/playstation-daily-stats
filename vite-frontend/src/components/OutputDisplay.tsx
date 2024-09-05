import { useState, useEffect } from "react";

interface LocalizedName {
  defaultLanguage: string;
  metadata: {
    [key: string]: string; // For all language codes like 'ar-AE', 'da-DK', etc.
  };
}

interface Image {
  format: string;
  type: string;
  url: string;
}

interface Media {
  audios: []; // Assuming it's an array, but type is not specified
  images: Image[];
  videos: []; // Assuming it's an array, but type is not specified
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

function OutputDisplay() {
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

  if (error) return <div className="text-red-500">Error: {error}</div>;
  if (!data) return <div className="text-gray-500">Loading...</div>;

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

  return (
    <div className="container mx-auto px-4 py-8">
      <h1 className="text-4xl font-bold mb-8">Game List</h1>
      <ul className="grid grid-cols-1 md:grid-cols-2 gap-8">
        {data.titles.filter((title => title.category.includes("game"))).map((title, index) => (
          <li key={index} className="bg-gray-800 rounded-lg p-6 shadow-lg">
            <h2 className="text-2xl font-semibold mb-4">{title.name}</h2>
            <div className="flex flex-col">
              <img
                src={title.localizedImageUrl}
                alt={title.name}
                className="w-full rounded-lg mb-4"
              />
              <div>
                <p className="text-gray-300 mb-2">Play Count: {title.playCount}</p>
                <p className="text-gray-300 mb-2">Play Duration: {formatPlayDuration(title.playDuration)}</p>
                <p className="text-gray-300 mb-2">First Played: {formatDate(title.firstPlayedDateTime)}</p>
                <p className="text-gray-300">Last Played: {formatDate(title.lastPlayedDateTime)}</p>
              </div>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

export default OutputDisplay;
