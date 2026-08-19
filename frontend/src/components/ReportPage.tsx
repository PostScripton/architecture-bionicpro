import React, { useEffect, useState } from 'react';

const API_URL = process.env.REACT_APP_API_URL || '';

interface Report {
  status: 'ready' | 'pending';
  message?: string;
  report_url?: string;
  username?: string;
  report_date?: string;
  full_name?: string;
  region?: string;
  prosthetic_model?: string;
  events_count?: number;
  avg_response_time_ms?: number;
  avg_signal_quality?: number;
  avg_battery_level?: number;
  error_count?: number;
  updated_at?: string;
}

const ReportPage: React.FC = () => {
  const [checkingSession, setCheckingSession] = useState(true);
  const [authenticated, setAuthenticated] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [report, setReport] = useState<Report | null>(null);

  useEffect(() => {
    const checkSession = async () => {
      try {
        const response = await fetch(`${API_URL}/auth/session`, {
          credentials: 'include',
        });
        setAuthenticated(response.ok);
      } catch {
        setAuthenticated(false);
      } finally {
        setCheckingSession(false);
      }
    };

    checkSession();
  }, []);

  const login = () => {
    // Full page navigation: the browser needs to follow the redirect chain
    // through bionicpro-auth and Keycloak. The frontend never handles an
    // authorization code or a token - only the resulting session cookie.
    window.location.href = `${API_URL}/auth/login`;
  };

  const logout = async () => {
    await fetch(`${API_URL}/auth/logout`, {
      method: 'POST',
      credentials: 'include',
    });
    setAuthenticated(false);
  };

  const downloadReport = async () => {
    try {
      setLoading(true);
      setError(null);
      setReport(null);

      const response = await fetch(`${API_URL}/reports`, {
        credentials: 'include',
      });

      if (response.status === 401) {
        setAuthenticated(false);
        return;
      }

      if (response.status === 403) {
        setError('У вас нет доступа к отчётам по протезам');
        return;
      }

      if (!response.ok && response.status !== 202) {
        throw new Error(`Request failed with status ${response.status}`);
      }

      const data: Report = await response.json();

      // reports-api больше не отдаёт содержимое отчёта напрямую - только
      // ссылку на CDN (Nginx перед S3/Minio), которая кеширует статический
      // JSON и раздаёт его без повторной нагрузки на reports-api и OLAP.
      if (data.status === 'ready' && data.report_url) {
        const reportResponse = await fetch(data.report_url);
        if (!reportResponse.ok) {
          throw new Error(`Failed to fetch report from CDN: ${reportResponse.status}`);
        }
        const reportBody: Report = await reportResponse.json();
        setReport(reportBody);
        return;
      }

      setReport(data);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
  };

  if (checkingSession) {
    return <div>Loading...</div>;
  }

  if (!authenticated) {
    return (
      <div className="flex flex-col items-center justify-center min-h-screen bg-gray-100">
        <button
          onClick={login}
          className="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600"
        >
          Login
        </button>
      </div>
    );
  }

  return (
    <div className="flex flex-col items-center justify-center min-h-screen bg-gray-100">
      <div className="p-8 bg-white rounded-lg shadow-md">
        <h1 className="text-2xl font-bold mb-6">Usage Reports</h1>

        <button
          onClick={downloadReport}
          disabled={loading}
          className={`px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600 ${
            loading ? 'opacity-50 cursor-not-allowed' : ''
          }`}
        >
          {loading ? 'Generating Report...' : 'Download Report'}
        </button>

        <button
          onClick={logout}
          className="ml-4 px-4 py-2 bg-gray-300 text-gray-800 rounded hover:bg-gray-400"
        >
          Logout
        </button>

        {error && (
          <div className="mt-4 p-4 bg-red-100 text-red-700 rounded">
            {error}
          </div>
        )}

        {report && report.status === 'pending' && (
          <div className="mt-4 p-4 bg-yellow-100 text-yellow-800 rounded">
            Отчёт ещё не готов, попробуйте позже
          </div>
        )}

        {report && report.status === 'ready' && (
          <div className="mt-4 p-4 bg-gray-50 rounded border border-gray-200 text-left">
            <p><strong>Пользователь:</strong> {report.full_name}</p>
            <p><strong>Регион:</strong> {report.region}</p>
            <p><strong>Модель протеза:</strong> {report.prosthetic_model}</p>
            <p><strong>Отчёт за:</strong> {report.report_date}</p>
            <p><strong>Событий:</strong> {report.events_count}</p>
            <p><strong>Среднее время отклика:</strong> {report.avg_response_time_ms} мс</p>
            <p><strong>Качество сигнала:</strong> {report.avg_signal_quality}%</p>
            <p><strong>Заряд батареи:</strong> {report.avg_battery_level}%</p>
            <p><strong>Ошибок:</strong> {report.error_count}</p>
          </div>
        )}
      </div>
    </div>
  );
};

export default ReportPage;
