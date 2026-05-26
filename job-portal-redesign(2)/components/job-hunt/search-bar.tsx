'use client'

import { useState, useRef } from 'react'

interface SearchBarProps {
  value: string
  onChange: (value: string) => void
  onFileUpload: (file: File) => void
  isUploading: boolean
}

export default function SearchBar({ value, onChange, onFileUpload, isUploading }: SearchBarProps) {
  const [isDragging, setIsDragging] = useState(false)
  const [errorMsg, setErrorMsg] = useState('')
  const fileInputRef = useRef<HTMLInputElement>(null)

  const handleDragOver = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(true)
  }

  const handleDragLeave = () => {
    setIsDragging(false)
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    setIsDragging(false)
    setErrorMsg('')
    if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
      handleFile(e.dataTransfer.files[0])
    }
  }

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    setErrorMsg('')
    if (e.target.files && e.target.files.length > 0) {
      handleFile(e.target.files[0])
      // Reset input so the same file can be selected again
      e.target.value = ''
    }
  }

  const handleFile = (file: File) => {
    if (file.size > 2 * 1024 * 1024) {
      setErrorMsg('File size must be under 2MB')
      return
    }
    if (file.type !== 'application/pdf' && !file.name.toLowerCase().endsWith('.pdf')) {
      setErrorMsg('Please upload a PDF file')
      return
    }
    onFileUpload(file)
  }

  return (
    <div className="mb-8 max-w-4xl mx-auto flex flex-col md:flex-row gap-4 items-start">
      <div className="relative flex-1 w-full">
        <input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="Search job titles, companies, locations..."
          className="w-full px-5 py-3 text-base border border-white/40 bg-white/60 backdrop-blur-md rounded-xl shadow-lg focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-all"
        />
      </div>
      
      <div className="w-full md:w-auto">
        <div 
          onClick={() => fileInputRef.current?.click()}
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
          className={`
            relative overflow-hidden cursor-pointer flex items-center justify-center gap-2 px-6 py-3
            rounded-xl border-2 border-dashed transition-all duration-300
            ${isDragging ? 'border-blue-500 bg-blue-50/50 scale-105' : 'border-gray-300 bg-white/40 hover:bg-white/60'}
            backdrop-blur-md shadow-sm min-w-[200px]
          `}
        >
          <input 
            type="file" 
            ref={fileInputRef} 
            onChange={handleFileChange} 
            accept=".pdf" 
            className="hidden" 
          />
          
          {isUploading ? (
            <>
              <svg className="animate-spin -ml-1 mr-2 h-5 w-5 text-blue-500" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <span className="font-medium text-blue-500">Analyzing...</span>
            </>
          ) : (
            <>
              <span className="text-xl">📄</span>
              <span className="font-medium text-gray-600 whitespace-nowrap hidden sm:inline">Drop resume here or click to match skills</span>
              <span className="font-medium text-gray-600 whitespace-nowrap sm:hidden">Match Resume</span>
            </>
          )}
        </div>
        {errorMsg && (
          <div className="mt-2 text-sm text-red-500 font-medium px-1 animate-pulse text-center">
            {errorMsg}
          </div>
        )}
      </div>
    </div>
  )
}
