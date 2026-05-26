'use client'

import { useState } from 'react'

interface QueueItem {
  title: string
  company: string
  tag: string
  url: string
}

interface QueueSidebarProps {
  applyQueue: Map<string, QueueItem>
  onRemoveFromQueue: (url: string) => void
}

export default function QueueSidebar({
  applyQueue,
  onRemoveFromQueue,
}: QueueSidebarProps) {
  const [isOpen, setIsOpen] = useState(false)
  const [isPowerMode, setIsPowerMode] = useState(false)
  const [currentJobIndex, setCurrentJobIndex] = useState(0)

  const jobs = Array.from(applyQueue.values())
  const queueCount = jobs.length

  const handleStartBulkApply = () => {
    if (queueCount === 0) return
    setIsPowerMode(true)
    setCurrentJobIndex(0)
    const firstJob = jobs[0]
    window.open(firstJob.url, 'apply_tab')
  }

  const handleNextJob = () => {
    if (currentJobIndex < jobs.length - 1) {
      setCurrentJobIndex(currentJobIndex + 1)
      const nextJob = jobs[currentJobIndex + 1]
      const tab = window.open(nextJob.url, 'apply_tab')
      if (!tab) {
        alert('Pop-up blocker prevented opening. Please allow pop-ups.')
      }
    } else {
      alert('You\'ve reached the end of your queue!')
    }
  }

  const handlePrevJob = () => {
    if (currentJobIndex > 0) {
      setCurrentJobIndex(currentJobIndex - 1)
      const prevJob = jobs[currentJobIndex - 1]
      const tab = window.open(prevJob.url, 'apply_tab')
      if (!tab) {
        alert('Pop-up blocker prevented opening. Please allow pop-ups.')
      }
    }
  }

  const currentJob = jobs[currentJobIndex]

  return (
    <>
      {/* Power Mode Top Bar */}
      {isPowerMode && (
        <div className="fixed top-0 w-full bg-gray-900 text-white z-50 px-6 py-4 shadow-lg">
          <div className="flex flex-col sm:flex-row justify-between items-center gap-4">
            <div className="font-bold text-lg flex items-center gap-2">
              <span>⚡</span> Bulk Apply Mode
            </div>
            <div className="flex items-center gap-3">
              <span className="text-sm font-medium">
                Job {currentJobIndex + 1} of {queueCount} • {currentJob?.company}
              </span>
              <button
                onClick={handlePrevJob}
                className="px-3 py-1.5 text-sm bg-gray-700 hover:bg-gray-600 rounded font-bold transition"
              >
                ⬅️ Back
              </button>
              <button
                onClick={handleNextJob}
                className="px-4 py-1.5 text-sm bg-blue-600 hover:bg-blue-500 rounded font-bold transition"
              >
                Next ➡️
              </button>
              <button
                onClick={() => setIsPowerMode(false)}
                className="px-3 py-1.5 text-sm font-bold text-red-400 hover:text-red-300 hover:bg-red-400/10 rounded transition"
              >
                Exit
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Floating Queue Button */}
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="fixed bottom-6 right-6 bg-gray-900 text-white px-5 py-4 rounded-full shadow-2xl hover:bg-gray-800 z-30 transition transform hover:scale-105 flex items-center gap-2 font-bold border-2 border-gray-700"
      >
        <span>📥 Queue ({queueCount})</span>
      </button>

      {/* Sidebar Overlay */}
      {isOpen && (
        <div
          className="fixed inset-0 bg-black/20 z-30 backdrop-blur-sm transition-opacity"
          onClick={() => setIsOpen(false)}
        />
      )}

      {/* Sidebar */}
      <div
        className={`fixed right-0 top-0 h-full w-80 sm:w-96 bg-white shadow-2xl transform transition-transform duration-300 z-40 flex flex-col border-l border-gray-200 ${
          isOpen ? 'translate-x-0' : 'translate-x-full'
        }`}
      >
        {/* Header */}
        <div className="p-5 bg-gray-900 text-white flex justify-between items-center shadow">
          <h2 className="font-bold text-xl flex items-center gap-2">
            📥 Apply Queue
            <span className="bg-gray-700 px-2.5 py-0.5 rounded-lg text-sm ml-1">
              {queueCount}
            </span>
          </h2>
          <button
            onClick={() => setIsOpen(false)}
            className="text-2xl font-light hover:scale-110 transition"
          >
            ×
          </button>
        </div>

        {/* Queue List */}
        <div className="flex-1 overflow-y-auto p-4 space-y-3 bg-gray-50">
          {queueCount === 0 ? (
            <div className="text-center text-gray-400 py-10">
              <div className="text-4xl mb-3">👻</div>
              <p className="font-medium">Your queue is empty</p>
              <p className="text-sm">Click "Queue" on jobs to add them</p>
            </div>
          ) : (
            jobs.map((job) => (
              <div
                key={job.url}
                className="bg-white p-3 rounded-lg border border-gray-100 shadow-sm relative group hover:shadow-md transition"
              >
                <div className="font-bold text-sm text-gray-900 leading-tight mb-1">
                  {job.title}
                </div>
                <div className="text-xs text-gray-500">
                  {job.company} • {job.tag}
                </div>
                <button
                  onClick={() => onRemoveFromQueue(job.url)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-gray-300 hover:text-red-500 text-xl font-bold transition"
                >
                  ×
                </button>
              </div>
            ))
          )}
        </div>

        {/* Footer Button */}
        <div className="p-5 border-t bg-white shadow-lg">
          <button
            onClick={handleStartBulkApply}
            disabled={queueCount === 0}
            className={`w-full py-4 font-bold rounded-lg shadow transition flex justify-center items-center gap-2 text-lg ${
              queueCount === 0
                ? 'bg-gray-200 text-gray-400 cursor-not-allowed'
                : 'bg-gray-900 text-white hover:bg-gray-800'
            }`}
          >
            <span>Start Bulk Apply</span>
            <span>🚀</span>
          </button>
        </div>
      </div>
    </>
  )
}
