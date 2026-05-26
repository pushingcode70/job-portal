import Link from 'next/link'

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
  confidenceScore?: number
  matchedSkills?: string[]
}

interface CompanyCardProps {
  company: Company
  queuedURLs: Set<string>
  onAddToQueue: (job: Job, company: Company) => void
  searchQuery: string
}

export default function CompanyCard({
  company,
  queuedURLs,
  onAddToQueue,
  searchQuery,
}: CompanyCardProps) {
  const visibleJobs = company.jobs.slice(0, 3)
  const moreJobsCount = Math.max(0, company.jobs.length - 3)

  const getCompanyProfileUrl = () => {
    const baseUrl = `/company/${encodeURIComponent(company.name)}`
    if (searchQuery && searchQuery.toLowerCase() !== company.name.toLowerCase()) {
      return `${baseUrl}?search=${encodeURIComponent(searchQuery)}`
    }
    return baseUrl
  }

  return (
    <div className="bg-white rounded-lg shadow-sm border border-gray-200 hover:shadow-md transition-all duration-200 overflow-hidden flex flex-col h-full">
      {/* Company Header */}
      <div className="p-5 border-b border-gray-100">
        <div className="flex items-start gap-3">
          <div className="w-12 h-12 bg-gradient-to-br from-blue-500 to-blue-600 rounded-lg flex items-center justify-center text-white font-bold text-lg flex-shrink-0">
            {company.name.charAt(0).toUpperCase()}
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              <h3 className="text-lg font-bold text-gray-900 truncate capitalize">
                {company.name}
              </h3>
              {company.confidenceScore !== undefined && (
                <span className="inline-flex items-center gap-1 bg-blue-50 text-blue-700 text-[10px] font-bold px-2 py-0.5 rounded-full border border-blue-100 whitespace-nowrap">
                  <span className="w-1.5 h-1.5 rounded-full bg-blue-500 animate-pulse"></span>
                  {company.confidenceScore}% Match
                </span>
              )}
            </div>
            {company.isIndian && (
              <span className="text-xs font-bold mt-1 inline-block uppercase tracking-wide text-green-600">
                🇮🇳 India
              </span>
            )}
          </div>
        </div>
        
        {/* Matched Skills */}
        {company.matchedSkills && company.matchedSkills.length > 0 && (
          <div className="mt-3 pt-3 border-t border-gray-100">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs font-bold text-gray-500 mr-1">Matches:</span>
              {company.matchedSkills.slice(0, 5).map(skill => (
                <span key={skill} className="bg-green-50 text-green-700 border border-green-200 text-[10px] font-bold px-2 py-0.5 rounded uppercase tracking-wider">
                  {skill}
                </span>
              ))}
              {company.matchedSkills.length > 5 && (
                <span className="text-[10px] font-bold text-gray-400">+{company.matchedSkills.length - 5}</span>
              )}
            </div>
          </div>
        )}
      </div>

      {/* Jobs List */}
      <div className="flex-1 p-5 space-y-3">
        {visibleJobs.map((job) => {
          const isQueued = queuedURLs.has(job.url)
          return (
            <div
              key={job.url}
              className="bg-gray-50 rounded-lg p-3 border border-gray-100 hover:border-gray-200 transition-colors"
            >
              <div className="flex gap-2 items-start justify-between">
                <div className="flex-1 min-w-0">
                  <p className="font-semibold text-sm text-gray-900 break-words flex items-center flex-wrap gap-2">
                    <span>{job.title}</span>
                    {job.isNew && (
                      <span className="inline-flex items-center gap-1 text-[10px] bg-amber-50 text-amber-700 border border-amber-200 px-1.5 py-0.5 rounded font-bold uppercase tracking-wider">
                        🕒 New
                      </span>
                    )}
                  </p>
                  <div className="flex flex-wrap gap-1.5 mt-2">
                    <span className={`text-[10px] font-bold px-1.5 py-0.5 rounded inline-block uppercase tracking-wider ${
                      job.isIndia
                        ? 'bg-green-100 text-green-700 border border-green-200'
                        : 'bg-gray-100 text-gray-700 border border-gray-200'
                    }`}>
                      {job.locationTag}
                    </span>
                    {job.isRemote && (
                      <span className="text-[10px] font-bold px-1.5 py-0.5 rounded inline-block uppercase tracking-wider bg-purple-50 text-purple-700 border border-purple-200">
                        🏠 Remote
                      </span>
                    )}
                  </div>
                </div>
                <button
                  onClick={() => onAddToQueue(job, company)}
                  disabled={isQueued}
                  className={`text-xs px-2 py-1 rounded font-semibold whitespace-nowrap flex-shrink-0 border transition-colors ${
                    isQueued
                      ? 'bg-green-50 text-green-700 border-green-200 cursor-default'
                      : 'bg-blue-50 text-blue-700 border-blue-200 hover:bg-blue-100'
                  }`}
                >
                  {isQueued ? '✓ Added' : '➕ Queue'}
                </button>
              </div>
            </div>
          )
        })}

        {moreJobsCount > 0 && (
          <p className="text-xs text-center text-gray-500 font-medium py-2">
            + {moreJobsCount} more roles available
          </p>
        )}
      </div>

      {/* Footer Button */}
      <div className="border-t border-gray-100 p-4">
        <Link
          href={getCompanyProfileUrl()}
          className="block w-full px-4 py-2.5 bg-gray-900 text-white text-sm font-bold rounded-lg hover:bg-gray-800 transition-colors text-center"
        >
          View Company Profile
        </Link>
      </div>
    </div>
  )
}
