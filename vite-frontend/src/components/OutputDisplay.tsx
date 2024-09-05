import { useState, useEffect } from "react";

interface Title {
  name: string;
  imageUrl: string;
  playCount: number;
  playDuration: string;
}

interface Data {
  titles: Title[];
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

  if (error) return <div>Error: {error}</div>;
  if (!data) return <div>Loading...</div>;

  // Function to convert PT format to hours and minutes
  const formatPlayDuration = (duration: string) => {
    const match = duration.match(/PT(\d+H)?(\d+M)?(\d+S)?/);
    if (!match) return "Unknown";

    const hours = parseInt(match[1] || "0");
    const minutes = parseInt(match[2] || "0");

    return `${hours}h ${minutes}m`;
  };

  return (
    <div>
      <h1>Game List</h1>
      <ul style={{ listStyle: "none", padding: 0 }}>
        {data.titles.map((title, index) => (
          <li
            key={index}
            style={{
              marginBottom: "20px",
              borderBottom: "1px solid #ccc",
              paddingBottom: "10px",
            }}
          >
            <h2>{title.name}</h2>
            <img
              src={title.imageUrl}
              alt={title.name}
              style={{ maxWidth: "200px" }}
            />
            <p>Play Count: {title.playCount}</p>
            <p>Play Duration: {formatPlayDuration(title.playDuration)}</p>
          </li>
        ))}
      </ul>
    </div>
  );
}

export default OutputDisplay;
