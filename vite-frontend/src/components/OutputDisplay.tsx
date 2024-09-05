import { useState, useEffect } from 'react'
import fs from 'fs'
import path from 'path'

function OutputDisplay() {
  const [data, setData] = useState<any>(null)

  useEffect(() => {
    const outputDir = '/app/output'
    fs.readdir(outputDir, (err, files) => {
      if (err) {
        console.error('Error reading directory:', err)
        return
      }

      // Get the most recent file
      const latestFile = files.sort().reverse()[0]
      if (latestFile) {
        fs.readFile(path.join(outputDir, latestFile), 'utf8', (err, content) => {
          if (err) {
            console.error('Error reading file:', err)
            return
          }
          setData(JSON.parse(content))
        })
      }
    })
  }, [])

  if (!data) return <div>Loading...</div>

  return (
    <div>
      <h1>Latest Output</h1>
      <pre>{JSON.stringify(data, null, 2)}</pre>
    </div>
  )
}

export default OutputDisplay