import { useState, useEffect } from 'react'

function OutputDisplay() {
  const [data, setData] = useState<any>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    fetch('http://localhost:8080/api/latest-output')
      .then(response => {
        if (!response.ok) {
          throw new Error('Network response was not ok')
        }
        return response.json()
      })
      .then(data => setData(data))
      .catch(error => {
        console.error('Error fetching data:', error)
        setError('Failed to fetch data')
      })
  }, [])

  if (error) return <div>Error: {error}</div>
  if (!data) return <div>Loading...</div>

  return (
    <div>
      <h1>Latest Output</h1>
      <pre>{JSON.stringify(data, null, 2)}</pre>
    </div>
  )
}

export default OutputDisplay