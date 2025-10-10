import { GamesListV2 } from "./components/games-list-v2";

declare global {
  interface Number {
    nth(): string;
  }
}

Number.prototype.nth = function (this: number): string {
  const n = this % 10;
  return n == 1 && this != 11 ? "st" : n == 2 ? "nd" : n == 3 ? "rd" : "th";
};

function App() {
  return (
    <div className="App">
      <GamesListV2 />
    </div>
  );
}

export default App;
