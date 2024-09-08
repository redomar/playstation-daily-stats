import { GamesList } from "./components/games-list";

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
      {/* <OutputDisplay /> */}
      <GamesList />
    </div>
  );
}

export default App;
