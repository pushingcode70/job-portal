'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import CompanyCard from '@/components/job-hunt/company-card'
import QueueSidebar from '@/components/job-hunt/queue-sidebar'
import SearchBar from '@/components/job-hunt/search-bar'
import FilterButtons from '@/components/job-hunt/filter-buttons'

interface Job {
  title: string
  location: string
  locationTag: string
  url: string
  isIndia: boolean
  isNew?: boolean
  isRemote?: boolean
}

interface Company {
  name: string
  isIndian: boolean
  jobs: Job[]
}

export default function Home() {
  const [searchQuery, setSearchQuery] = useState('')
  const [baseResults, setBaseResults] = useState<Company[]>([])
  const [allResults, setAllResults] = useState<Company[]>([])
  const [isLoading, setIsLoading] = useState(false)
  const [isResumeMode, setIsResumeMode] = useState(false)
  const [isUploading, setIsUploading] = useState(false)
  const [uploadError, setUploadError] = useState('')
  const [renderPage, setRenderPage] = useState(0)
  const [applyQueue, setApplyQueue] = useState(new Map())
  const [queuedURLs, setQueuedURLs] = useState(new Set())
  const [currentResume, setCurrentResume] = useState<File | null>(null)
  const [remoteOnly, setRemoteOnly] = useState(false)
  const [recentOnly, setRecentOnly] = useState(false)
  const [indiaOnly, setIndiaOnly] = useState(false)
  const [discoveryStats, setDiscoveryStats] = useState({ totalRuntimeCompanies: 0, linkedInSlugsFound: 0, linkedInVerified: 0, indianVerified: 0 })
  const [isDiscovering, setIsDiscovering] = useState(false)
  const discoveryTimerRef = useRef<NodeJS.Timeout>()
  const debounceTimer = useRef<NodeJS.Timeout>()
  const observerRef = useRef<IntersectionObserver>()
  const scrollTriggerRef = useRef<HTMLDivElement>(null)

  const BATCH_SIZE = 40

  // Search companies
  const searchCompanies = useCallback(async (query: string, remote = remoteOnly, recent = recentOnly, india = indiaOnly) => {
    setIsLoading(true)
    setRenderPage(0)
    setBaseResults([])
    setAllResults([])

    try {
      const response = await fetch(`/api/companies?search=${encodeURIComponent(query)}&remoteOnly=${remote}&recentOnly=${recent}&indiaOnly=${india}`)
      const companies = await response.json()
      
      setBaseResults(companies || [])
      setAllResults(companies || [])
    } catch (error) {
      console.error('Search failed:', error)
      setBaseResults([])
      setAllResults([])
    } finally {
      setIsLoading(false)
    }
  }, [])

  // Debounced search — also marks discovery as active for queries > 2 chars
  useEffect(() => {
    if (debounceTimer.current) clearTimeout(debounceTimer.current)
    
    debounceTimer.current = setTimeout(() => {
      if (!isResumeMode) {
        searchCompanies(searchQuery, remoteOnly, recentOnly, indiaOnly)
        // Signal live discovery is running for long-enough queries
        if (searchQuery.trim().length >= 2) {
          setIsDiscovering(true)
          // Auto-clear after 90s (max time for Serper + ATS probing)
          if (discoveryTimerRef.current) clearTimeout(discoveryTimerRef.current)
          discoveryTimerRef.current = setTimeout(() => setIsDiscovering(false), 90_000)
        } else {
          setIsDiscovering(false)
        }
      }
    }, 250)

    return () => clearTimeout(debounceTimer.current)
  }, [searchQuery, searchCompanies, isResumeMode, remoteOnly, recentOnly, indiaOnly])

  // Poll discovery stats — fast (3s) while discovering, slow (15s) otherwise
  useEffect(() => {
    const fetchStats = async () => {
      try {
        const res = await fetch('/api/discovery-stats')
        if (res.ok) {
          const data = await res.json()
          setDiscoveryStats(data)
        }
      } catch {}
    }
    fetchStats()
    const interval = setInterval(fetchStats, isDiscovering ? 3_000 : 15_000)
    return () => clearInterval(interval)
  }, [isDiscovering])

  // File Upload Handler
  const handleFileUpload = async (file: File, remote = remoteOnly, recent = recentOnly) => {
    setCurrentResume(file)
    setIsUploading(true)
    setSearchQuery('')
    setIsResumeMode(true)
    setUploadError('')
    setBaseResults([])
    setAllResults([])
    setRenderPage(0)

    const formData = new FormData()
    formData.append('resume', file)

    try {
      const response = await fetch(`/api/match-resume?remoteOnly=${remote}&recentOnly=${recent}&indiaOnly=${indiaOnly}`, {
        method: 'POST',
        body: formData,
      })
      
      const data = await response.json()
      if (!response.ok) {
        throw new Error(data.error || 'Failed to match resume')
      }
      
      setBaseResults(data || [])
      setAllResults(data || [])
    } catch (error) {
      console.error('Resume match failed:', error)
      setBaseResults([])
      setAllResults([])
      setUploadError(error instanceof Error ? error.message : 'An error occurred')
      setIsResumeMode(false)
    } finally {
      setIsUploading(false)
    }
  }

  // Trigger search on toggle
  useEffect(() => {
    if (isResumeMode && currentResume) {
      handleFileUpload(currentResume, remoteOnly, recentOnly)
    } else {
      searchCompanies(searchQuery, remoteOnly, recentOnly, indiaOnly)
    }
  }, [remoteOnly, recentOnly, indiaOnly])

  // Set query from button clicks
  const setQueryFromButton = (query: string) => {
    setSearchQuery(query)
  }

  // Add to queue
  const addToQueue = (job: Job, company: Company) => {
    if (queuedURLs.has(job.url)) return
    
    const newQueue = new Map(applyQueue)
    newQueue.set(job.url, { ...job, company: company.name })
    
    const newQueuedURLs = new Set(queuedURLs)
    newQueuedURLs.add(job.url)
    
    setApplyQueue(newQueue)
    setQueuedURLs(newQueuedURLs)
  }

  const isDiscoveryMode = !searchQuery.trim() && !isResumeMode

  // Remove from queue
  const removeFromQueue = (url: string) => {
    const newQueue = new Map(applyQueue)
    newQueue.delete(url)
    
    const newQueuedURLs = new Set(queuedURLs)
    newQueuedURLs.delete(url)
    
    setApplyQueue(newQueue)
    setQueuedURLs(newQueuedURLs)
  }

  // Render next batch
  const renderedCompanies = allResults.slice(0, (renderPage + 1) * BATCH_SIZE)

  // Intersection Observer for infinite scroll
  useEffect(() => {
    if (!scrollTriggerRef.current) return

    observerRef.current = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && allResults.length > ((renderPage + 1) * BATCH_SIZE)) {
          setRenderPage(prev => prev + 1)
        }
      },
      { rootMargin: '200px' }
    )

    observerRef.current.observe(scrollTriggerRef.current)
    return () => observerRef.current?.disconnect()
  }, [renderPage, allResults.length])

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <nav className="bg-white border-b border-gray-200 sticky top-0 z-10">
        <div className="max-w-5xl mx-auto px-4 py-4 flex justify-between items-center">
          <h1 className="text-2xl font-bold text-gray-900">Job Hunt</h1>
          <a 
            href="#" 
            className="px-4 py-2 text-sm font-medium text-gray-700 bg-gray-100 rounded-lg hover:bg-gray-200 transition-colors"
          >
            Sign In
          </a>
        </div>
      </nav>

      {/* Main Content */}
      <div className="max-w-5xl mx-auto py-8 px-4">
        {/* Hero Section */}
        <div className="text-center mb-12">
          <h2 className="text-4xl md:text-5xl font-bold text-gray-900 mb-4">
            Find Your Next Job
          </h2>
          <p className="text-lg text-gray-600 max-w-2xl mx-auto">
            Search across thousands of job openings from top companies.
          </p>
        </div>

        {/* Search Bar */}
        <SearchBar 
          value={searchQuery} 
          onChange={setSearchQuery} 
          onFileUpload={handleFileUpload}
          isUploading={isUploading}
        />

        {uploadError && (
          <div className="text-center text-red-500 mb-6 font-medium bg-red-50 max-w-lg mx-auto py-3 px-4 rounded-xl shadow-sm border border-red-100">
            ⚠️ {uploadError}
          </div>
        )}

        {/* Toggle Filters */}
        <div className="flex flex-wrap justify-center gap-3 mt-6 mb-8">
          <button
            onClick={() => setRemoteOnly(!remoteOnly)}
            className={`px-4 py-2 rounded-full font-medium text-sm transition-all border ${
              remoteOnly 
                ? 'bg-blue-600 text-white border-blue-600 shadow-md transform scale-105' 
                : 'bg-white text-gray-700 border-gray-300 hover:bg-gray-50'
            }`}
          >
            🏠 Remote Only
          </button>
          <button
            onClick={() => setRecentOnly(!recentOnly)}
            className={`px-4 py-2 rounded-full font-medium text-sm transition-all border ${
              recentOnly 
                ? 'bg-blue-600 text-white border-blue-600 shadow-md transform scale-105' 
                : 'bg-white text-gray-700 border-gray-300 hover:bg-gray-50'
            }`}
          >
            🕒 Recently Added
          </button>
          <button
            onClick={() => setIndiaOnly(!indiaOnly)}
            className={`px-4 py-2 rounded-full font-medium text-sm transition-all border ${
              indiaOnly 
                ? 'bg-blue-600 text-white border-blue-600 shadow-md transform scale-105' 
                : 'bg-white text-gray-700 border-gray-300 hover:bg-gray-50'
            }`}
          >
            🇮🇳 India Only
          </button>
        </div>


        {isResumeMode && !isUploading && !isLoading && (
          <div className="text-center mb-8">
            <button 
              onClick={() => {
                setIsResumeMode(false)
                setBaseResults([])
                setAllResults([])
              }}
              className="text-sm font-medium text-gray-600 hover:text-gray-900 bg-white border border-gray-300 hover:bg-gray-50 px-5 py-2.5 rounded-xl shadow-sm transition-all"
            >
              ✕ Clear Resume Matches
            </button>
          </div>
        )}

        {/* Filter Buttons */}
        <FilterButtons 
          onSetQuery={setQueryFromButton}
        />

        {/* Results Summary Bar */}
        {!isLoading && allResults.length > 0 && (
          <div className="flex items-center justify-between mb-4 px-1">
            <div className="flex items-center gap-3 flex-wrap">
              <span className="text-sm font-semibold text-gray-700">
                {allResults.length} {allResults.length === 1 ? 'company' : 'companies'}
              </span>
              <span className="text-xs text-gray-400">·</span>
              <span className="text-sm text-gray-500">
                {allResults.reduce((sum, c) => sum + c.jobs.length, 0).toLocaleString()} jobs
              </span>

              {/* Always show while actively discovering */}
              {searchQuery && isDiscovering && discoveryStats.totalRuntimeCompanies === 0 && (
                <>
                  <span className="text-xs text-gray-400">·</span>
                  <span className="inline-flex items-center gap-1.5 text-xs font-medium text-blue-600 bg-blue-50 border border-blue-200 px-2.5 py-1 rounded-full">
                    <svg className="animate-spin h-3 w-3 text-blue-500" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
                    </svg>
                    Searching web for more companies...
                  </span>
                </>
              )}


            </div>
            <span className="text-xs text-gray-400">
              Showing {Math.min(renderedCompanies.length, allResults.length)} of {allResults.length}
            </span>
          </div>
        )}

        {/* Results Grid */}
        {isLoading && (
          <div className="flex justify-center items-center py-20">
            <div className="text-gray-600 font-medium">Loading...</div>
          </div>
        )}

        {!isLoading && renderedCompanies.length === 0 && (searchQuery || isResumeMode) && (
          <div className="text-center py-20">
            <div className="text-5xl mb-4">🔍</div>
            <h3 className="text-2xl font-bold text-gray-700 mb-2">No jobs found</h3>
            <p className="text-lg text-gray-500 mb-6">Try a different search term or remove filters</p>
            <button
              onClick={() => {
                setSearchQuery('')
                setIsResumeMode(false)
                setRemoteOnly(false)
                setRecentOnly(false)
                setIndiaOnly(false)
              }}
              className="px-6 py-2 bg-blue-600 text-white font-medium rounded-full hover:bg-blue-700 transition-colors shadow-sm"
            >
              Clear Search
            </button>
          </div>
        )}

        {renderedCompanies.length > 0 && (
          <div className="mb-8" style={{ animation: 'fadeIn 0.6s ease-out' }}>
            <style>{`
              @keyframes fadeIn {
                from { opacity: 0; transform: translateY(15px); }
                to { opacity: 1; transform: translateY(0); }
              }
            `}</style>
            <div className="mb-4 text-gray-600 font-medium flex items-center justify-between">
              {isDiscoveryMode ? (
                <span className="text-xl font-bold text-gray-800">✨ Featured Opportunities</span>
              ) : (
                <span>Found {allResults.length} {allResults.length === 1 ? 'company' : 'companies'} {isResumeMode && 'matching the skills in your resume'}</span>
              )}
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {renderedCompanies.map((company) => (
                <CompanyCard
                  key={company.name}
                  company={company}
                  queuedURLs={queuedURLs}
                  onAddToQueue={addToQueue}
                  searchQuery={searchQuery}
                />
              ))}
            </div>
          </div>
        )}

        {/* Infinite Scroll Trigger */}
        <div ref={scrollTriggerRef} className="h-20 flex items-center justify-center">
          {allResults.length > renderedCompanies.length && (
            <div className="text-gray-400">Loading more...</div>
          )}
        </div>
      </div>

      {/* Queue Sidebar */}
      <QueueSidebar
        applyQueue={applyQueue}
        onRemoveFromQueue={removeFromQueue}
      />
    </div>
  )
}
